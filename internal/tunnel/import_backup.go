package tunnel

// import_backup.go — import a Cascade or AWG-Easy JSON backup.
//
// Two formats are supported:
//
//  1. Cascade format (produced by GET /:id/backup):
//     { "interface": { privateKey, publicKey, address, ... }, "peers": [...] }
//
//  2. AWG-Easy format (migration from AWG-Easy):
//     { "server": { privateKey, publicKey, address, jc, ... }, "clients": { uuid: {...} } }
//
// ImportBackup auto-detects the format by the presence of the "interface" key.
// Keys (server + client) are used as-is — no regeneration.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/JohnnyVBut/cascade/internal/peer"
	"github.com/JohnnyVBut/cascade/internal/validate"
)

// ── Backup JSON types ─────────────────────────────────────────────────────────

// cascadeBackup is the top-level structure of a Cascade backup file.
type cascadeBackup struct {
	Interface cascadeBackupIface `json:"interface"`
	Peers     []cascadeBackupPeer `json:"peers"`
}

// cascadeBackupIface mirrors the ifaceJSON output plus privateKey.
type cascadeBackupIface struct {
	PrivateKey    string           `json:"privateKey"`
	PublicKey     string           `json:"publicKey"`
	Address       string           `json:"address"`
	Protocol      string           `json:"protocol"`
	DisableRoutes bool             `json:"disableRoutes"`
	NatDisabled   bool             `json:"natDisabled"`
	DNS           string           `json:"dns"`
	PublicHost    string           `json:"publicHost"`
	MTU           int              `json:"mtu"`
	MSS           int              `json:"mss"`
	AWG2          *peer.AWG2Settings `json:"settings"`
}

// cascadeBackupPeer mirrors the peer.Peer JSON fields we need.
type cascadeBackupPeer struct {
	Name                string      `json:"name"`
	PublicKey           string      `json:"publicKey"`
	PrivateKey          string      `json:"privateKey"`
	PresharedKey        string      `json:"presharedKey"`
	AllowedIPs          string      `json:"allowedIPs"`
	Address             string      `json:"address"`
	ClientAllowedIPs    string      `json:"clientAllowedIPs"`
	PeerType            string      `json:"peerType"`
	Endpoint            string      `json:"endpoint"`
	PersistentKeepalive int         `json:"persistentKeepalive"`
	GroupID             string      `json:"groupId"`
	ExpiredAt           interface{} `json:"expiredAt"`
	Enabled             bool        `json:"enabled"`
	CreatedAt           string      `json:"createdAt"`
}

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

// ImportBackupInput is the payload for Manager.ImportBackup (AWG-Easy format).
type ImportBackupInput struct {
	RawJSON    string // raw content of the AWG-Easy backup JSON file
	ListenPort int    // UDP port to assign to the new interface
}

// ImportInterfaceInput is the payload for Manager.ImportInterface (Cascade export format).
type ImportInterfaceInput struct {
	RawJSON    string // raw content of a Cascade export JSON (GET /:id/export)
	ListenPort int    // UDP port to assign to the restored interface
}

// ImportBackupResult is returned by Manager.ImportBackup and Manager.ImportInterface.
type ImportBackupResult struct {
	Interface    *TunnelInterface
	PeersCreated int
	PeersFailed  []string // names of clients that could not be imported
	Started      bool
	StartError   error
}

// ImportInterface restores a Cascade interface export (produced by GET /:id/export).
// It creates a new interface with the original keys and optionally recreates peers.
func (m *Manager) ImportInterface(inp ImportInterfaceInput) (*ImportBackupResult, error) {
	if inp.ListenPort <= 0 || inp.ListenPort > 65535 {
		return nil, fmt.Errorf("invalid listen port %d", inp.ListenPort)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(inp.RawJSON), &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, ok := raw["interface"]; !ok {
		return nil, fmt.Errorf("not a Cascade export: missing \"interface\" key")
	}
	return m.importCascadeBackup(ImportBackupInput{RawJSON: inp.RawJSON, ListenPort: inp.ListenPort}, raw)
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
	return m.importAWGEasyBackup(inp)
}

// importCascadeBackup handles the native Cascade backup format:
// { "interface": { privateKey, publicKey, address, ... }, "peers": [...] }
func (m *Manager) importCascadeBackup(inp ImportBackupInput, raw map[string]json.RawMessage) (*ImportBackupResult, error) {
	var backup cascadeBackup
	if err := json.Unmarshal([]byte(inp.RawJSON), &backup); err != nil {
		return nil, fmt.Errorf("invalid Cascade backup JSON: %w", err)
	}

	ifc := backup.Interface
	if ifc.PrivateKey == "" {
		return nil, fmt.Errorf("backup missing interface.privateKey")
	}
	if ifc.PublicKey == "" {
		return nil, fmt.Errorf("backup missing interface.publicKey")
	}
	if ifc.Address == "" {
		return nil, fmt.Errorf("backup missing interface.address")
	}

	if err := validate.WGKey(ifc.PrivateKey); err != nil {
		return nil, fmt.Errorf("invalid server private key: %w", err)
	}
	if err := validate.WGKey(ifc.PublicKey); err != nil {
		return nil, fmt.Errorf("invalid server public key: %w", err)
	}

	address := ifc.Address
	if !strings.Contains(address, "/") {
		address += "/24"
	}

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

	protocol := ifc.Protocol
	if protocol == "" {
		protocol = "wireguard-1.0"
	}

	iface, err := m.CreateInterface(CreateInput{
		Protocol:      protocol,
		Address:       address,
		ListenPort:    inp.ListenPort,
		DisableRoutes: ifc.DisableRoutes,
		DNS:           ifc.DNS,
		AWG2:          ifc.AWG2,
	})
	if err != nil {
		return nil, fmt.Errorf("create interface: %w", err)
	}

	iface.PrivateKey = ifc.PrivateKey
	iface.PublicKey = ifc.PublicKey
	iface.NatDisabled = ifc.NatDisabled
	iface.PublicHost = ifc.PublicHost
	iface.MTU = ifc.MTU
	iface.MSS = ifc.MSS
	if err := iface.save(); err != nil {
		_ = m.DeleteInterface(iface.ID)
		return nil, fmt.Errorf("save interface keys: %w", err)
	}
	if err := iface.RegenerateConfig(); err != nil {
		_ = m.DeleteInterface(iface.ID)
		return nil, fmt.Errorf("regenerate config: %w", err)
	}

	var peersCreated int
	var peersFailed []string

	for _, bp := range backup.Peers {
		inp := peer.PeerInput{
			Name:                bp.Name,
			PublicKey:           bp.PublicKey,
			PrivateKey:          bp.PrivateKey,
			PresharedKey:        bp.PresharedKey,
			AllowedIPs:          bp.AllowedIPs,
			Address:             bp.Address,
			ClientAllowedIPs:    bp.ClientAllowedIPs,
			PeerType:            bp.PeerType,
			Endpoint:            bp.Endpoint,
			PersistentKeepalive: bp.PersistentKeepalive,
			GroupID:             bp.GroupID,
			GenerateKeys:        false,
			CreatedAt:           bp.CreatedAt,
		}
		if bp.ExpiredAt != nil {
			if s, ok := bp.ExpiredAt.(string); ok && s != "" {
				inp.ExpiredAt = s
			}
		}

		p, err := iface.AddPeer(inp)
		if err != nil {
			peersFailed = append(peersFailed, bp.Name)
			continue
		}
		if !bp.Enabled {
			f := false
			_, _ = iface.UpdatePeer(p.ID, peer.PeerUpdate{Enabled: &f})
		}
		peersCreated++
	}

	startErr := iface.Start()
	return &ImportBackupResult{
		Interface:    iface,
		PeersCreated: peersCreated,
		PeersFailed:  peersFailed,
		Started:      startErr == nil,
		StartError:   startErr,
	}, nil
}

// importAWGEasyBackup parses an AWG-Easy JSON backup and creates a new interface with
// all its clients. Keys (server + client) are used as-is — no regeneration.
// { "server": { privateKey, publicKey, address, jc, ... }, "clients": { uuid: {...} } }
func (m *Manager) importAWGEasyBackup(inp ImportBackupInput) (*ImportBackupResult, error) {
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
			CreatedAt:        client.CreatedAt, // preserve original creation time for stable sort
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
