package tunnel

// import_backup.go — import an AWG-Easy JSON backup file.
//
// AWG-Easy stores its data in a JSON file with two top-level keys:
//   - "server"  — interface private/public key + address + AWG2 params (strings)
//   - "clients" — map of UUID → client record (keys, address, enabled, …)
//
// ImportBackup creates a new interface using the server keys and address from
// the backup (no key regeneration), then recreates all clients.  The private
// keys of clients are preserved so that QR codes can be generated for them
// without reissuing configs.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/JohnnyVBut/cascade/internal/peer"
	"github.com/JohnnyVBut/cascade/internal/validate"
)

// ── Backup JSON types ─────────────────────────────────────────────────────────

// awgEasyBackup is the top-level structure of an AWG-Easy backup file.
type awgEasyBackup struct {
	Server  awgEasyServer            `json:"server"`
	Clients map[string]awgEasyClient `json:"clients"`
}

// awgEasyServer holds the WireGuard/AmneziaWG interface parameters.
// AWG2 obfuscation fields (Jc, Jmin, Jmax, S1-S4, H1-H4) are stored as
// strings in the backup — parseInt() converts them on import.
type awgEasyServer struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
	Address    string `json:"address"` // plain IP, no mask (e.g. "10.9.0.1")
	Jc         string `json:"jc"`
	Jmin       string `json:"jmin"`
	Jmax       string `json:"jmax"`
	S1         string `json:"s1"`
	S2         string `json:"s2"`
	S3         string `json:"s3"`
	S4         string `json:"s4"`
	H1         string `json:"h1"`
	H2         string `json:"h2"`
	H3         string `json:"h3"`
	H4         string `json:"h4"`
}

// awgEasyClient holds a single client record from the backup.
// ExpiredAt is interface{} because it may be null or a date string.
type awgEasyClient struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Address      string      `json:"address"` // plain IP, no mask (e.g. "10.9.0.2")
	PrivateKey   string      `json:"privateKey"`
	PublicKey    string      `json:"publicKey"`
	PreSharedKey string      `json:"preSharedKey"`
	CreatedAt    string      `json:"createdAt"`
	UpdatedAt    string      `json:"updatedAt"`
	ExpiredAt    interface{} `json:"expiredAt"`
	Enabled      bool        `json:"enabled"`
}

// ── Input / Result types ──────────────────────────────────────────────────────

// ImportBackupInput is the payload for Manager.ImportBackup.
type ImportBackupInput struct {
	RawJSON    string // raw content of the AWG-Easy backup JSON file
	ListenPort int    // UDP port to assign to the new interface
}

// ImportBackupResult is returned by Manager.ImportBackup.
type ImportBackupResult struct {
	Interface    *TunnelInterface
	PeersCreated int
	PeersFailed  []string // names of clients that could not be imported
	Started      bool
	StartError   error
}

// ── ImportBackup ──────────────────────────────────────────────────────────────

// ImportBackup parses an AWG-Easy JSON backup and creates a new interface with
// all its clients.  Keys (server + client) are used as-is — no regeneration.
//
// Conflict rules (hard errors, no partial import):
//   - listen port already used by another interface
//   - server address subnet overlaps with an existing interface
//
// All clients are created with GenerateKeys=false so their private keys are
// preserved for later QR / config download.  Disabled clients in the backup
// are created as disabled in Cascade.
func (m *Manager) ImportBackup(inp ImportBackupInput) (*ImportBackupResult, error) {
	// ── Parse JSON ────────────────────────────────────────────────────────────
	var backup awgEasyBackup
	if err := json.Unmarshal([]byte(inp.RawJSON), &backup); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	srv := backup.Server
	if srv.PrivateKey == "" {
		return nil, fmt.Errorf("backup missing server.privateKey")
	}
	if srv.PublicKey == "" {
		return nil, fmt.Errorf("backup missing server.publicKey")
	}
	if srv.Address == "" {
		return nil, fmt.Errorf("backup missing server.address")
	}
	if inp.ListenPort <= 0 || inp.ListenPort > 65535 {
		return nil, fmt.Errorf("invalid listen port %d", inp.ListenPort)
	}

	// Validate server keys before any shell use (injection prevention).
	if err := validate.WGKey(srv.PrivateKey); err != nil {
		return nil, fmt.Errorf("invalid server private key: %w", err)
	}
	if err := validate.WGKey(srv.PublicKey); err != nil {
		return nil, fmt.Errorf("invalid server public key: %w", err)
	}

	// Normalise address: AWG-Easy stores plain IPs without a mask → add /24.
	address := srv.Address
	if !strings.Contains(address, "/") {
		address += "/24"
	}

	// ── Conflict checks ───────────────────────────────────────────────────────
	m.mu.RLock()
	for _, t := range m.interfaces {
		if t.ListenPort == inp.ListenPort {
			m.mu.RUnlock()
			return nil, fmt.Errorf("port %d is already used by interface %s", inp.ListenPort, t.ID)
		}
		if subnetsOverlap(t.Address, address) {
			m.mu.RUnlock()
			return nil, fmt.Errorf("address %s overlaps with existing interface %s (%s)", address, t.ID, t.Address)
		}
	}
	m.mu.RUnlock()

	// ── Detect protocol ───────────────────────────────────────────────────────
	// AWG-Easy uses AWG2 when Jc (junk-count) is set.
	protocol := "wireguard-1.0"
	var awg2 *peer.AWG2Settings
	if srv.Jc != "" {
		protocol = "amneziawg-2.0"
		awg2 = parseAWGEasyParams(srv)
	}

	// ── Create interface ──────────────────────────────────────────────────────
	// CreateInterface generates a throwaway key pair; we replace it below with
	// the backup keys.  This is the same pattern used by ImportConf.
	iface, err := m.CreateInterface(CreateInput{
		Protocol:      protocol,
		Address:       address,
		ListenPort:    inp.ListenPort,
		DisableRoutes: false,
		AWG2:          awg2,
	})
	if err != nil {
		return nil, fmt.Errorf("create interface: %w", err)
	}

	// Override auto-generated keys with the backup values.
	iface.PrivateKey = srv.PrivateKey
	iface.PublicKey = srv.PublicKey
	if err := iface.save(); err != nil {
		_ = m.DeleteInterface(iface.ID)
		return nil, fmt.Errorf("save interface keys: %w", err)
	}
	if err := iface.RegenerateConfig(); err != nil {
		_ = m.DeleteInterface(iface.ID)
		return nil, fmt.Errorf("regenerate config: %w", err)
	}

	// ── Import clients ────────────────────────────────────────────────────────
	// Derive the mask bits from the interface address so peer Address fields
	// use the same prefix length (e.g. /24).
	ifaceMask := "24"
	if parts := strings.SplitN(address, "/", 2); len(parts) == 2 {
		ifaceMask = parts[1]
	}

	var peersCreated int
	var peersFailed []string

	for _, client := range backup.Clients {
		// Strip mask if present (AWG-Easy stores plain IPs, but be safe).
		peerIP := strings.SplitN(client.Address, "/", 2)[0]
		if peerIP == "" {
			peersFailed = append(peersFailed, client.Name)
			continue
		}

		peerInput := peer.PeerInput{
			Name:             client.Name,
			PublicKey:        client.PublicKey,
			PrivateKey:       client.PrivateKey,
			PresharedKey:     client.PreSharedKey,
			AllowedIPs:       peerIP + "/32",
			Address:          peerIP + "/" + ifaceMask,
			ClientAllowedIPs: "0.0.0.0/0",
			PeerType:         "client",
			GenerateKeys:     false,
		}

		p, err := iface.AddPeer(peerInput)
		if err != nil {
			peersFailed = append(peersFailed, client.Name)
			continue
		}

		// Propagate disabled state from backup.
		if !client.Enabled {
			f := false
			_, _ = iface.UpdatePeer(p.ID, peer.PeerUpdate{Enabled: &f})
		}
		peersCreated++
	}

	// ── Start interface ───────────────────────────────────────────────────────
	startErr := iface.Start()

	return &ImportBackupResult{
		Interface:    iface,
		PeersCreated: peersCreated,
		PeersFailed:  peersFailed,
		Started:      startErr == nil,
		StartError:   startErr,
	}, nil
}

// parseAWGEasyParams converts the string-typed AWG2 fields from an AWG-Easy
// backup into peer.AWG2Settings.  Unknown / zero values are left at zero.
func parseAWGEasyParams(srv awgEasyServer) *peer.AWG2Settings {
	atoi := func(s string) int {
		n, _ := strconv.Atoi(strings.TrimSpace(s))
		return n
	}
	return &peer.AWG2Settings{
		Jc:   atoi(srv.Jc),
		Jmin: atoi(srv.Jmin),
		Jmax: atoi(srv.Jmax),
		S1:   atoi(srv.S1),
		S2:   atoi(srv.S2),
		S3:   atoi(srv.S3),
		S4:   atoi(srv.S4),
		H1:   strings.TrimSpace(srv.H1),
		H2:   strings.TrimSpace(srv.H2),
		H3:   strings.TrimSpace(srv.H3),
		H4:   strings.TrimSpace(srv.H4),
	}
}
