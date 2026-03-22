// Package ipset manages kernel ipsets used by FirewallManager for alias matching.
// Port of IpsetManager.js.
//
// Ipsets are persisted to DATA_DIR/*.save files and restored on startup via
// "ipset restore -!" so firewall rules survive container restarts.
//
// Async generation jobs (fetchCountry / fetchASN) run in goroutines and are
// tracked in a sync.Map keyed by a random hex job-ID.
package ipset

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/JohnnyVBut/awg-easy/internal/util"
)

// JobStatus represents the current state of an async prefix-generation job.
type JobStatus struct {
	Status     string `json:"status"`              // "running" | "done" | "error" | "unknown"
	EntryCount int    `json:"entryCount,omitempty"` // populated when status == "done"
	Error      string `json:"error,omitempty"`      // populated when status == "error"
}

// GenerateOpts controls which prefixes to fetch.
// Exactly one of Country, ASN, or ASNList must be set.
type GenerateOpts struct {
	Country string // 2-letter ISO country code, e.g. "RU"
	ASN     string // single ASN, e.g. "AS12345" or "12345"
	ASNList string // comma-separated ASNs, e.g. "12345,20485,3216"
}

// Manager manages kernel ipsets for firewall aliases.
type Manager struct {
	dataDir string
	jobs    sync.Map // jobID (string) → JobStatus
}

var nameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,30}$`)

// New creates a Manager, ensures the data directory exists, and restores saved ipsets.
func New(dataDir string) (*Manager, error) {
	m := &Manager{dataDir: dataDir}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("ipset: mkdir %s: %w", dataDir, err)
	}
	m.RestoreAll()
	return m, nil
}

// RestoreAll loads all *.save files from the data directory into the kernel.
// Errors are logged but not returned — a missing ipset is non-fatal at startup.
func (m *Manager) RestoreAll() {
	entries, err := os.ReadDir(m.dataDir)
	if err != nil {
		log.Printf("ipset: restoreAll readdir %s: %v", m.dataDir, err)
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".save") {
			continue
		}
		filePath := filepath.Join(m.dataDir, e.Name())
		if _, err := util.ExecDefault(fmt.Sprintf("ipset restore -! < %s", filePath)); err != nil {
			log.Printf("ipset: restore %s: %v", e.Name(), err)
		} else {
			log.Printf("ipset: restored %s", e.Name())
		}
	}
}

// CreateSet creates an ipset of type hash:net (idempotent via -exist).
func (m *Manager) CreateSet(name string) error {
	if err := m.validateName(name); err != nil {
		return err
	}
	_, err := util.ExecDefault(fmt.Sprintf("ipset create %s hash:net family inet -exist", name))
	return err
}

// DestroySet destroys the named ipset and removes its save file.
// Errors from the kernel (e.g. set does not exist) are silently ignored.
func (m *Manager) DestroySet(name string) error {
	if err := m.validateName(name); err != nil {
		return err
	}
	util.ExecSilent(fmt.Sprintf("ipset destroy %s", name)) //nolint:errcheck
	os.Remove(filepath.Join(m.dataDir, name+".save"))      //nolint:errcheck
	return nil
}

// LoadFromFile loads CIDRs from a plain-text file (one per line, # comments ignored)
// into the named ipset using an atomic swap via a temporary set.
// Returns the number of entries loaded.
func (m *Manager) LoadFromFile(name, filePath string) (int, error) {
	if err := m.validateName(name); err != nil {
		return 0, err
	}
	tmpName := name + "_tmp"

	cleanup := func() {
		util.ExecSilent(fmt.Sprintf("ipset destroy %s 2>/dev/null || true", tmpName)) //nolint:errcheck
	}

	// (Re)create the tmp set.
	util.ExecSilent(fmt.Sprintf("ipset destroy %s 2>/dev/null || true", tmpName)) //nolint:errcheck
	if _, err := util.ExecDefault(fmt.Sprintf("ipset create %s hash:net family inet -exist", tmpName)); err != nil {
		return 0, fmt.Errorf("create tmp set: %w", err)
	}

	// Read source file.
	data, err := os.ReadFile(filePath)
	if err != nil {
		cleanup()
		return 0, fmt.Errorf("read %s: %w", filePath, err)
	}

	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}

	// Build an ipset restore script for batch add (much faster than individual adds).
	var sb strings.Builder
	fmt.Fprintf(&sb, "create %s hash:net family inet -exist\n", tmpName)
	for _, cidr := range lines {
		fmt.Fprintf(&sb, "add %s %s -exist\n", tmpName, cidr)
	}

	tmpScript := filepath.Join(m.dataDir, name+"_restore.tmp")
	if err := os.WriteFile(tmpScript, []byte(sb.String()), 0644); err != nil {
		cleanup()
		return 0, fmt.Errorf("write restore script: %w", err)
	}
	defer os.Remove(tmpScript) //nolint:errcheck

	if _, err := util.ExecDefault(fmt.Sprintf("ipset restore -! < %s", tmpScript)); err != nil {
		cleanup()
		return 0, fmt.Errorf("ipset restore: %w", err)
	}

	// Ensure target set exists before the atomic swap.
	if _, err := util.ExecDefault(fmt.Sprintf("ipset create %s hash:net family inet -exist", name)); err != nil {
		cleanup()
		return 0, err
	}

	// Atomic swap: live set instantly gets all new entries.
	if _, err := util.ExecDefault(fmt.Sprintf("ipset swap %s %s", tmpName, name)); err != nil {
		cleanup()
		return 0, fmt.Errorf("ipset swap: %w", err)
	}
	util.ExecSilent(fmt.Sprintf("ipset destroy %s 2>/dev/null || true", tmpName)) //nolint:errcheck

	return len(lines), nil
}

// SaveSet persists the named ipset to disk so it survives container restarts.
func (m *Manager) SaveSet(name string) error {
	if err := m.validateName(name); err != nil {
		return err
	}
	saveFile := filepath.Join(m.dataDir, name+".save")
	_, err := util.ExecDefault(fmt.Sprintf("ipset save %s > %s", name, saveFile))
	return err
}

// GetEntryCount returns the number of entries in the named ipset (0 on error).
func (m *Manager) GetEntryCount(name string) int {
	out, err := util.ExecFast(fmt.Sprintf("ipset list %s -t 2>/dev/null", name))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(out, "\n") {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "Number of entries: %d", &n); err == nil {
			return n
		}
	}
	return 0
}

// RunGenerator starts an async job that fetches prefixes from RIPEstat and loads
// them into the named ipset. Returns a job ID that can be polled with GetJobStatus.
func (m *Manager) RunGenerator(name string, opts GenerateOpts) (string, error) {
	if err := m.validateName(name); err != nil {
		return "", err
	}
	jobID := randomHex(8)
	m.jobs.Store(jobID, JobStatus{Status: "running"})
	go m.runGeneratorAsync(jobID, name, opts)
	return jobID, nil
}

// GetJobStatus returns the current status of a generation job.
func (m *Manager) GetJobStatus(jobID string) JobStatus {
	if v, ok := m.jobs.Load(jobID); ok {
		return v.(JobStatus)
	}
	return JobStatus{Status: "unknown"}
}

// ── Private ───────────────────────────────────────────────────────────────────

func (m *Manager) runGeneratorAsync(jobID, name string, opts GenerateOpts) {
	outFile := filepath.Join(m.dataDir, name+"_generated.txt")

	var (
		prefixes []string
		err      error
	)
	switch {
	case opts.ASNList != "":
		prefixes, err = fetchASNList(opts.ASNList)
	case opts.ASN != "":
		prefixes, err = fetchASN(opts.ASN)
	case opts.Country != "":
		prefixes, err = fetchCountry(opts.Country)
	default:
		err = fmt.Errorf("GenerateOpts must include Country, ASN, or ASNList")
	}

	if err != nil {
		m.jobs.Store(jobID, JobStatus{Status: "error", Error: err.Error()})
		return
	}
	if len(prefixes) == 0 {
		m.jobs.Store(jobID, JobStatus{Status: "error", Error: "no prefixes returned — check source params"})
		return
	}

	if err := writeToFile(prefixes, outFile); err != nil {
		m.jobs.Store(jobID, JobStatus{Status: "error", Error: err.Error()})
		return
	}

	if err := m.CreateSet(name); err != nil {
		os.Remove(outFile) //nolint:errcheck
		m.jobs.Store(jobID, JobStatus{Status: "error", Error: err.Error()})
		return
	}

	entryCount, err := m.LoadFromFile(name, outFile)
	os.Remove(outFile) //nolint:errcheck
	if err != nil {
		m.jobs.Store(jobID, JobStatus{Status: "error", Error: err.Error()})
		return
	}

	if err := m.SaveSet(name); err != nil {
		log.Printf("ipset: job %s: SaveSet failed: %v", jobID, err)
	}

	m.jobs.Store(jobID, JobStatus{Status: "done", EntryCount: entryCount})
	log.Printf("ipset: job %s done — %d entries loaded into %s", jobID, entryCount, name)
}

func (m *Manager) validateName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid ipset name: %q", name)
	}
	return nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}
