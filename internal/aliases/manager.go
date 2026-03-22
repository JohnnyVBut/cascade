// Package aliases manages named address/port sets used as Firewall Aliases.
// Port of AliasManager.js.
//
// Six alias types:
//
//	host       — one or more IPv4 addresses ("1.2.3.4")
//	network    — one or more CIDR prefixes ("192.168.0.0/16")
//	ipset      — kernel ipset (hash:net), large sets; data lives in ipsets/*.save
//	group      — merges entries from multiple host/network aliases (deduplicated)
//	port       — L4 ports: "tcp:443", "udp:53", "any:80", "tcp:8080-8090"
//	port-group — merges entries from multiple port aliases
//
// Persistence: SQLite `aliases` table (see internal/db migration v2).
// Ipset kernel objects are managed via internal/ipset.Manager.
package aliases

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/ipset"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Alias represents a named address/port set.
type Alias struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Type          string         `json:"type"`           // host/network/ipset/group/port/port-group
	Entries       []string       `json:"entries"`        // for host/network/port
	MemberIDs     []string       `json:"memberIds"`      // for group/port-group
	IPSetName     string         `json:"ipsetName,omitempty"` // for ipset
	EntryCount    int            `json:"entryCount"`
	GeneratorOpts *GeneratorOpts `json:"generatorOpts"` // null unless generated via RIPEstat
	LastUpdated   string         `json:"lastUpdated,omitempty"`
	CreatedAt     string         `json:"createdAt"`
}

// GeneratorOpts stores the source parameters used to generate an ipset alias.
type GeneratorOpts struct {
	Country string `json:"country,omitempty"`
	ASN     string `json:"asn,omitempty"`
	ASNList string `json:"asnList,omitempty"`
}

// MatchSpec is returned by GetMatchSpec for use in FirewallManager iptables rules.
type MatchSpec struct {
	Type    string   `json:"type"`              // "ipset" or "cidr"
	Name    string   `json:"name,omitempty"`    // set when Type == "ipset"
	Entries []string `json:"entries,omitempty"` // set when Type == "cidr"
}

// PortMatchSpec describes one protocol group for firewall port matching.
type PortMatchSpec struct {
	Proto     string `json:"proto"`     // "tcp" or "udp"
	Ports     string `json:"ports"`     // e.g. "80,443" or "8080:8090"
	Multiport bool   `json:"multiport"` // true when Ports contains more than one port/range
}

// Manager manages alias CRUD and integrates with IpsetManager for ipset aliases.
type Manager struct {
	ipsetMgr *ipset.Manager
}

var aliasNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

// New creates a Manager backed by the given IpsetManager.
// db.Init() must have been called before New().
func New(im *ipset.Manager) *Manager {
	return &Manager{ipsetMgr: im}
}

// ── Public CRUD ───────────────────────────────────────────────────────────────

// GetAll returns all aliases ordered by created_at ASC.
func (m *Manager) GetAll() ([]Alias, error) {
	rows, err := db.DB().Query(`
		SELECT id, name, description, type, entries, member_ids, ipset_name,
		       entry_count, generator_opts, last_updated, created_at
		FROM aliases ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("aliases query: %w", err)
	}
	defer rows.Close()

	var out []Alias
	for rows.Next() {
		a, err := scanAliasRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, nil
}

// GetByID returns the alias with the given id, or nil if not found.
func (m *Manager) GetByID(id string) (*Alias, error) {
	return queryAlias(`WHERE id = ?`, id)
}

// GetByName returns the alias with the given name (case-insensitive), or nil if not found.
func (m *Manager) GetByName(name string) (*Alias, error) {
	return queryAlias(`WHERE name = ? COLLATE NOCASE`, name)
}

// Create creates a new alias.
func (m *Manager) Create(data Alias) (*Alias, error) {
	if err := validateName(data.Name); err != nil {
		return nil, err
	}
	if err := validateType(data.Type); err != nil {
		return nil, err
	}

	// Unique name check.
	existing, err := m.GetByName(data.Name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("alias name %q already exists", data.Name)
	}

	isGroup := data.Type == "group"
	isIPSet := data.Type == "ipset"
	isPort := data.Type == "port"
	isPortGroup := data.Type == "port-group"
	isPlain := !isGroup && !isIPSet && !isPort && !isPortGroup

	// Validate and normalise members / entries.
	var memberIDs []string
	var entries []string

	if isGroup || isPortGroup {
		allowedMemberTypes := []string{"host", "network"}
		if isPortGroup {
			allowedMemberTypes = []string{"port"}
		}
		if err := m.validateMembers(data.MemberIDs, allowedMemberTypes); err != nil {
			return nil, err
		}
		memberIDs = dedupeStrings(data.MemberIDs)
	}

	if isPlain {
		entries = normalizeEntries(data.Entries)
	}
	if isPort {
		entries, err = normalizePortEntries(data.Entries)
		if err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	a := Alias{
		ID:          uuid.NewString(),
		Name:        strings.TrimSpace(data.Name),
		Description: strings.TrimSpace(data.Description),
		Type:        data.Type,
		Entries:     entries,
		MemberIDs:   memberIDs,
		IPSetName:   "",
		EntryCount:  0,
		CreatedAt:   now,
	}

	if isIPSet {
		a.IPSetName = ipsetNameFromAlias(a.Name)
	}
	if isPlain || isPort {
		a.EntryCount = len(a.Entries)
		if a.EntryCount > 0 {
			a.LastUpdated = now
		}
	}
	if isGroup || isPortGroup {
		a.EntryCount = m.groupEntryCount(a.MemberIDs)
		if a.EntryCount > 0 {
			a.LastUpdated = now
		}
	}

	// Create kernel ipset.
	if isIPSet {
		if err := m.ipsetMgr.CreateSet(a.IPSetName); err != nil {
			return nil, fmt.Errorf("create ipset %s: %w", a.IPSetName, err)
		}
	}

	if err := insertAlias(&a); err != nil {
		return nil, err
	}

	log.Printf("aliases: created %s (%s, %s)", a.ID, a.Name, a.Type)
	return &a, nil
}

// Update applies partial updates to an existing alias.
// Type cannot be changed.
func (m *Manager) Update(id string, data Alias) (*Alias, error) {
	a, err := m.getOrNotFound(id)
	if err != nil {
		return nil, err
	}

	// Name change — check uniqueness.
	newName := strings.TrimSpace(data.Name)
	if newName != "" && newName != a.Name {
		existing, err := m.GetByName(newName)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.ID != id {
			return nil, fmt.Errorf("alias name %q already exists", newName)
		}
		if err := validateName(newName); err != nil {
			return nil, err
		}
		a.Name = newName
	}

	a.Description = strings.TrimSpace(data.Description)

	now := time.Now().UTC().Format(time.RFC3339)

	switch a.Type {
	case "host", "network":
		if data.Entries != nil {
			a.Entries = normalizeEntries(data.Entries)
			a.EntryCount = len(a.Entries)
			a.LastUpdated = now
		}
	case "port":
		if data.Entries != nil {
			a.Entries, err = normalizePortEntries(data.Entries)
			if err != nil {
				return nil, err
			}
			a.EntryCount = len(a.Entries)
			a.LastUpdated = now
		}
	case "group":
		if data.MemberIDs != nil {
			if err := m.validateMembers(data.MemberIDs, []string{"host", "network"}); err != nil {
				return nil, err
			}
			a.MemberIDs = dedupeStrings(data.MemberIDs)
			a.EntryCount = m.groupEntryCount(a.MemberIDs)
			a.LastUpdated = now
		}
	case "port-group":
		if data.MemberIDs != nil {
			if err := m.validateMembers(data.MemberIDs, []string{"port"}); err != nil {
				return nil, err
			}
			a.MemberIDs = dedupeStrings(data.MemberIDs)
			a.EntryCount = m.groupEntryCount(a.MemberIDs)
			a.LastUpdated = now
		}
	}

	if err := updateAlias(a); err != nil {
		return nil, err
	}

	log.Printf("aliases: updated %s", id)
	return a, nil
}

// Delete removes an alias.
// For ipset aliases, destroys the kernel set and .save file.
// Returns an error if the alias is referenced by a group.
func (m *Manager) Delete(id string) error {
	a, err := m.getOrNotFound(id)
	if err != nil {
		return err
	}

	// Check that no group references this alias.
	all, err := m.GetAll()
	if err != nil {
		return err
	}
	for _, g := range all {
		if g.Type != "group" && g.Type != "port-group" {
			continue
		}
		for _, mid := range g.MemberIDs {
			if mid == id {
				return fmt.Errorf("alias %q is used in group %q", a.Name, g.Name)
			}
		}
	}

	// Destroy kernel ipset if applicable.
	if a.Type == "ipset" && a.IPSetName != "" {
		if err := m.ipsetMgr.DestroySet(a.IPSetName); err != nil {
			log.Printf("aliases: destroySet %s on delete: %v", a.IPSetName, err)
		}
	}

	if _, err := db.DB().Exec(`DELETE FROM aliases WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete alias: %w", err)
	}

	log.Printf("aliases: deleted %s (%s)", id, a.Name)
	return nil
}

// ── Ipset: Upload & Generate ──────────────────────────────────────────────────

// UploadFromFile loads CIDRs from a plain-text file into an ipset alias.
// Only valid for type=ipset.
func (m *Manager) UploadFromFile(id, filePath string) (*Alias, error) {
	a, err := m.getOrNotFound(id)
	if err != nil {
		return nil, err
	}
	if a.Type != "ipset" {
		return nil, fmt.Errorf("upload only supported for ipset-type aliases")
	}

	count, err := m.ipsetMgr.LoadFromFile(a.IPSetName, filePath)
	if err != nil {
		return nil, fmt.Errorf("load ipset: %w", err)
	}
	if err := m.ipsetMgr.SaveSet(a.IPSetName); err != nil {
		log.Printf("aliases: saveSet %s after upload: %v", a.IPSetName, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	a.EntryCount = count
	a.LastUpdated = now
	a.GeneratorOpts = nil // manual upload — clear generator opts

	if err := updateAlias(a); err != nil {
		return nil, err
	}

	log.Printf("aliases: %s uploaded %d entries from file", id, count)
	return a, nil
}

// StartGenerate starts an async generation job for an ipset alias via RIPEstat.
// Returns a jobID to poll with GetJobStatus.
// Only valid for type=ipset.
func (m *Manager) StartGenerate(id string, opts GeneratorOpts) (string, error) {
	a, err := m.getOrNotFound(id)
	if err != nil {
		return "", err
	}
	if a.Type != "ipset" {
		return "", fmt.Errorf("generate only supported for ipset-type aliases")
	}

	// Persist generator opts for future reference.
	a.GeneratorOpts = &opts
	if err := updateAlias(a); err != nil {
		return "", err
	}

	jobID, err := m.ipsetMgr.RunGenerator(a.IPSetName, ipset.GenerateOpts{
		Country: opts.Country,
		ASN:     opts.ASN,
		ASNList: opts.ASNList,
	})
	if err != nil {
		return "", fmt.Errorf("run generator: %w", err)
	}

	// Background watcher: updates entryCount when the job completes.
	go m.watchJob(jobID, id)

	return jobID, nil
}

// GetJobStatus returns the status of a generation job.
func (m *Manager) GetJobStatus(jobID string) ipset.JobStatus {
	return m.ipsetMgr.GetJobStatus(jobID)
}

// FinalizeGeneration eagerly writes entryCount to the alias DB row when the
// job is done. Called from the HTTP polling handler so the DB is guaranteed to
// be up-to-date before the frontend receives the "done" response and calls
// loadAliases(). This fixes the race condition where watchJob's 2-second sleep
// interval causes the DB update to arrive after the frontend's 3-second poll.
// The write is idempotent — if watchJob already updated the row, this is a no-op.
func (m *Manager) FinalizeGeneration(aliasID string, entryCount int) {
	a, err := m.GetByID(aliasID)
	if err != nil || a == nil {
		return
	}
	if a.EntryCount == entryCount {
		return // already up to date
	}
	a.EntryCount = entryCount
	a.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	if err := updateAlias(a); err != nil {
		log.Printf("aliases: FinalizeGeneration %s: %v", aliasID, err)
	}
}

// ── Match specs (used by FirewallManager) ────────────────────────────────────

// GetMatchSpec returns the iptables match specification for an alias.
//
//	ipset aliases  → { Type: "ipset", Name: "ru_nets" }
//	host/network   → { Type: "cidr", Entries: ["10.0.0.0/8", ...] }
//	group          → { Type: "cidr", Entries: merged+deduplicated entries of all members }
func (m *Manager) GetMatchSpec(id string) (*MatchSpec, error) {
	a, err := m.getOrNotFound(id)
	if err != nil {
		return nil, err
	}

	switch a.Type {
	case "ipset":
		return &MatchSpec{Type: "ipset", Name: a.IPSetName}, nil

	case "group":
		all, err := m.GetAll()
		if err != nil {
			return nil, err
		}
		byID := make(map[string]*Alias, len(all))
		for i := range all {
			byID[all[i].ID] = &all[i]
		}
		seen := make(map[string]struct{})
		var merged []string
		for _, mid := range a.MemberIDs {
			member, ok := byID[mid]
			if !ok {
				continue
			}
			for _, e := range member.Entries {
				if _, exists := seen[e]; !exists {
					seen[e] = struct{}{}
					merged = append(merged, e)
				}
			}
		}
		return &MatchSpec{Type: "cidr", Entries: merged}, nil

	default: // host, network
		return &MatchSpec{Type: "cidr", Entries: a.Entries}, nil
	}
}

// GetPortMatchSpec returns port match specs for a port or port-group alias.
//
// Returns one entry per distinct protocol (tcp/udp), with ports joined by comma.
// Port ranges use iptables syntax: "8080:8090" (colon, not dash).
// "any:PORT" expands to both tcp:PORT and udp:PORT.
func (m *Manager) GetPortMatchSpec(id string) ([]PortMatchSpec, error) {
	a, err := m.getOrNotFound(id)
	if err != nil {
		return nil, err
	}

	var entries []string
	switch a.Type {
	case "port":
		entries = a.Entries
	case "port-group":
		all, err := m.GetAll()
		if err != nil {
			return nil, err
		}
		byID := make(map[string]*Alias, len(all))
		for i := range all {
			byID[all[i].ID] = &all[i]
		}
		for _, mid := range a.MemberIDs {
			if member, ok := byID[mid]; ok {
				entries = append(entries, member.Entries...)
			}
		}
	default:
		return nil, fmt.Errorf("alias %q is not a port alias (type=%s)", a.Name, a.Type)
	}

	// Group by protocol; "any" expands to tcp + udp.
	byProto := make(map[string][]string)
	for _, entry := range entries {
		m2 := portEntryRe.FindStringSubmatch(entry)
		if m2 == nil {
			continue
		}
		proto := strings.ToLower(m2[1])
		// Convert range dash to iptables colon: "8080-8090" → "8080:8090"
		port := strings.Replace(m2[2], "-", ":", 1)
		protos := []string{proto}
		if proto == "any" {
			protos = []string{"tcp", "udp"}
		}
		for _, p := range protos {
			byProto[p] = append(byProto[p], port)
		}
	}

	var result []PortMatchSpec
	for _, proto := range []string{"tcp", "udp"} { // deterministic order
		ports, ok := byProto[proto]
		if !ok {
			continue
		}
		deduped := dedupeStrings(ports)
		result = append(result, PortMatchSpec{
			Proto:     proto,
			Ports:     strings.Join(deduped, ","),
			Multiport: len(deduped) > 1,
		})
	}
	return result, nil
}

// ── Private helpers ───────────────────────────────────────────────────────────

var portEntryRe = regexp.MustCompile(`(?i)^(tcp|udp|any):(\d+(?:-\d+)?)$`)

func (m *Manager) getOrNotFound(id string) (*Alias, error) {
	a, err := m.GetByID(id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("alias not found")
	}
	return a, nil
}

func (m *Manager) validateMembers(memberIDs []string, allowedTypes []string) error {
	if len(memberIDs) == 0 {
		return fmt.Errorf("group alias must have at least one member")
	}
	for _, mid := range memberIDs {
		member, err := m.GetByID(mid)
		if err != nil {
			return err
		}
		if member == nil {
			return fmt.Errorf("member alias %s not found", mid)
		}
		found := false
		for _, t := range allowedTypes {
			if member.Type == t {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("member alias %q must be type %s (got %s)",
				member.Name, strings.Join(allowedTypes, " or "), member.Type)
		}
	}
	return nil
}

func (m *Manager) groupEntryCount(memberIDs []string) int {
	total := 0
	for _, mid := range memberIDs {
		a, err := m.GetByID(mid)
		if err == nil && a != nil {
			total += a.EntryCount
		}
	}
	return total
}

// watchJob polls until the generation job is complete, then updates entryCount in DB.
func (m *Manager) watchJob(jobID, aliasID string) {
	for {
		time.Sleep(2 * time.Second)
		status := m.ipsetMgr.GetJobStatus(jobID)
		if status.Status == "running" {
			continue
		}
		if status.Status == "done" {
			a, err := m.GetByID(aliasID)
			if err != nil || a == nil {
				return
			}
			a.EntryCount = status.EntryCount
			a.LastUpdated = time.Now().UTC().Format(time.RFC3339)
			if err := updateAlias(a); err != nil {
				log.Printf("aliases: watchJob %s: update failed: %v", jobID, err)
			} else {
				log.Printf("aliases: job %s done — alias %s now has %d entries", jobID, aliasID, a.EntryCount)
			}
		} else {
			log.Printf("aliases: job %s failed for alias %s: %s", jobID, aliasID, status.Error)
		}
		return
	}
}

// ── DB helpers ────────────────────────────────────────────────────────────────

func queryAlias(where string, args ...any) (*Alias, error) {
	row := db.DB().QueryRow(`
		SELECT id, name, description, type, entries, member_ids, ipset_name,
		       entry_count, generator_opts, last_updated, created_at
		FROM aliases `+where, args...)

	a, err := scanAliasRow(row)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// scannable is satisfied by both *sql.Row and *sql.Rows.
type scannable interface {
	Scan(dest ...any) error
}

func scanAliasRow(s scannable) (*Alias, error) {
	var (
		entriesJSON   string
		memberIDsJSON string
		generatorJSON string
		lastUpdated   string
	)
	var a Alias
	err := s.Scan(
		&a.ID, &a.Name, &a.Description, &a.Type,
		&entriesJSON, &memberIDsJSON, &a.IPSetName,
		&a.EntryCount, &generatorJSON, &lastUpdated, &a.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("alias scan: %w", err)
	}

	// Decode JSON fields.
	if err := json.Unmarshal([]byte(entriesJSON), &a.Entries); err != nil {
		a.Entries = []string{}
	}
	if err := json.Unmarshal([]byte(memberIDsJSON), &a.MemberIDs); err != nil {
		a.MemberIDs = []string{}
	}
	if generatorJSON != "" && generatorJSON != "{}" && generatorJSON != "null" {
		var g GeneratorOpts
		if err := json.Unmarshal([]byte(generatorJSON), &g); err == nil {
			a.GeneratorOpts = &g
		}
	}
	if a.Entries == nil {
		a.Entries = []string{}
	}
	if a.MemberIDs == nil {
		a.MemberIDs = []string{}
	}
	a.LastUpdated = lastUpdated

	return &a, nil
}

func insertAlias(a *Alias) error {
	entriesJSON, _ := json.Marshal(a.Entries)
	memberIDsJSON, _ := json.Marshal(a.MemberIDs)
	generatorJSON := marshalGeneratorOpts(a.GeneratorOpts)

	_, err := db.DB().Exec(`
		INSERT INTO aliases
		    (id, name, description, type, entries, member_ids, ipset_name,
		     entry_count, generator_opts, last_updated, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.Description, a.Type,
		string(entriesJSON), string(memberIDsJSON), a.IPSetName,
		a.EntryCount, generatorJSON, a.LastUpdated, a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert alias: %w", err)
	}
	return nil
}

func updateAlias(a *Alias) error {
	entriesJSON, _ := json.Marshal(a.Entries)
	memberIDsJSON, _ := json.Marshal(a.MemberIDs)
	generatorJSON := marshalGeneratorOpts(a.GeneratorOpts)

	_, err := db.DB().Exec(`
		UPDATE aliases SET
		    name=?, description=?, entries=?, member_ids=?, ipset_name=?,
		    entry_count=?, generator_opts=?, last_updated=?
		WHERE id=?`,
		a.Name, a.Description,
		string(entriesJSON), string(memberIDsJSON), a.IPSetName,
		a.EntryCount, generatorJSON, a.LastUpdated,
		a.ID,
	)
	if err != nil {
		return fmt.Errorf("update alias: %w", err)
	}
	return nil
}

func marshalGeneratorOpts(g *GeneratorOpts) string {
	if g == nil {
		return "null"
	}
	b, _ := json.Marshal(g)
	return string(b)
}

// ── Validation helpers ────────────────────────────────────────────────────────

func validateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("alias name is required")
	}
	if !aliasNameRe.MatchString(name) {
		return fmt.Errorf("alias name must start with a letter and contain only letters, digits, _ or - (got %q)", name)
	}
	return nil
}

func validateType(t string) error {
	switch t {
	case "host", "network", "ipset", "group", "port", "port-group":
		return nil
	}
	return fmt.Errorf("alias type must be host, network, ipset, group, port, or port-group (got %q)", t)
}

func normalizeEntries(entries []string) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

// normalizePortEntries validates and normalises port entries.
// Format: "proto:port" or "proto:start-end"
// proto: tcp | udp | any
// port:  1–65535
func normalizePortEntries(entries []string) ([]string, error) {
	var result []string
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		m := portEntryRe.FindStringSubmatch(entry)
		if m == nil {
			return nil, fmt.Errorf("invalid port entry %q — format: tcp:443 / udp:53 / any:80 / tcp:8080-8090", entry)
		}
		proto := strings.ToLower(m[1])
		portStr := m[2]

		if strings.Contains(portStr, "-") {
			parts := strings.SplitN(portStr, "-", 2)
			a, b := atoi(parts[0]), atoi(parts[1])
			if a < 1 || a > 65535 {
				return nil, fmt.Errorf("port out of range in %q (must be 1-65535)", entry)
			}
			if b < 1 || b > 65535 {
				return nil, fmt.Errorf("port out of range in %q (must be 1-65535)", entry)
			}
			if b <= a {
				return nil, fmt.Errorf("invalid range in %q (end must be greater than start)", entry)
			}
			result = append(result, fmt.Sprintf("%s:%d-%d", proto, a, b))
		} else {
			p := atoi(portStr)
			if p < 1 || p > 65535 {
				return nil, fmt.Errorf("port out of range in %q (must be 1-65535)", entry)
			}
			result = append(result, fmt.Sprintf("%s:%d", proto, p))
		}
	}
	return result, nil
}

// ipsetNameFromAlias derives an ipset kernel name from alias name.
// Rules: lowercase, "-" → "_", truncate to 31 chars (ipset kernel limit).
func ipsetNameFromAlias(name string) string {
	s := strings.ToLower(strings.ReplaceAll(name, "-", "_"))
	if len(s) > 31 {
		s = s[:31]
	}
	return s
}

func dedupeStrings(s []string) []string {
	seen := make(map[string]struct{}, len(s))
	out := s[:0:0]
	for _, v := range s {
		if _, exists := seen[v]; !exists {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func atoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

// ── Singleton accessor ────────────────────────────────────────────────────────

var instance *Manager

// SetInstance stores the initialized Manager for package-level access.
// Must be called from main() before serving requests.
func SetInstance(m *Manager) { instance = m }

// Get returns the package-level Manager singleton.
// Panics with a clear message if SetInstance was not called (programming error).
func Get() *Manager {
	if instance == nil {
		panic("aliases: manager not initialized — call SetInstance before Get()")
	}
	return instance
}
