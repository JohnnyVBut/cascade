// Manager (InterfaceManager) — singleton that owns the in-memory collection of
// all TunnelInterface instances, drives their lifecycle, and polls status.
//
// Init sequence (FIX-13):
//  1. db.Init() must complete first.
//  2. tunnel.Init() loads all interfaces from SQLite and auto-starts enabled ones.
//  3. Caller (main.go) then invokes routing.RestoreAll() and nat.Init()
//     so that routes/NAT rules are applied after the interfaces exist in the kernel.
//
// Status polling: a background goroutine calls TunnelInterface.GetStatus() every second
// on all enabled interfaces (updates runtime fields: TransferRx/Tx, handshake, endpoint).
// The goroutine is stopped by calling Manager.Stop() on graceful shutdown.
package tunnel

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/JohnnyVBut/cascade/internal/peer"
	"github.com/JohnnyVBut/cascade/internal/settings"
)

// Manager manages the collection of all TunnelInterface instances.
type Manager struct {
	mu         sync.RWMutex
	interfaces map[string]*TunnelInterface

	stopCh chan struct{} // closed by Stop() to end the polling goroutine

	WGHost string // WG_HOST value — used in ExportInterfaceParams calls
}

// CreateInput is the payload for Manager.CreateInterface.
type CreateInput struct {
	Name          string
	Protocol      string             // default: "wireguard-1.0"
	Address       string             // CIDR e.g. "10.8.0.1/24"
	ListenPort    int                // 0 = auto-assign starting from 51830
	DisableRoutes bool
	AWG2          *peer.AWG2Settings // required for amneziawg-2.0
}

// ── Singleton ─────────────────────────────────────────────────────────────────

var (
	managerOnce sync.Once
	managerInst *Manager
	managerErr  error
)

// Init creates and initialises the singleton Manager.
// Must be called after db.Init().
// Loads all interfaces from SQLite and auto-starts those that were enabled.
// On success the polling goroutine starts; call Stop() on graceful shutdown.
func Init(wgHost string) (*Manager, error) {
	managerOnce.Do(func() {
		m := &Manager{
			interfaces: make(map[string]*TunnelInterface),
			stopCh:     make(chan struct{}),
			WGHost:     wgHost,
		}
		managerErr = m.load()
		if managerErr == nil {
			managerInst = m
			m.startPolling()
		}
	})
	return managerInst, managerErr
}

// Get returns the singleton Manager. Returns nil before Init() has been called.
func Get() *Manager {
	return managerInst
}

// Stop cancels the status polling goroutine. Call on graceful shutdown.
func (m *Manager) Stop() {
	close(m.stopCh)
}

// load reads all interfaces from SQLite and auto-starts enabled ones.
// Called once from Init(); not thread-safe (no concurrent callers yet).
func (m *Manager) load() error {
	ids, err := ListInterfaceIDs()
	if err != nil {
		return fmt.Errorf("list interfaces: %w", err)
	}

	for _, id := range ids {
		t, err := LoadInterface(id)
		if err != nil {
			log.Printf("tunnel: load interface %s: %v (skipping)", id, err)
			continue
		}
		m.interfaces[id] = t
	}

	log.Printf("tunnel: loaded %d interface(s) from DB", len(m.interfaces))

	// Auto-start interfaces that had enabled=true when the container last stopped.
	// If start fails, disable the interface so the UI shows the real state instead
	// of showing it as enabled while it is actually down.
	for id, t := range m.interfaces {
		if !t.Enabled {
			continue
		}
		if err := t.Start(); err != nil {
			log.Printf("tunnel: auto-start %s failed: %v (marking disabled)", id, err)
			t.Enabled = false
			_ = t.save()
		} else {
			log.Printf("tunnel: auto-started %s", id)
		}
	}

	return nil
}

// startPolling launches a goroutine that calls GetStatus on every enabled interface
// once per second. Stops when Stop() is called (closes stopCh).
func (m *Manager) startPolling() {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.mu.RLock()
				for _, t := range m.interfaces {
					t.GetStatus() // updates runtime peer fields; no-op when !t.Enabled
				}
				m.mu.RUnlock()
			case <-m.stopCh:
				return
			}
		}
	}()
}

// ── Interface CRUD ────────────────────────────────────────────────────────────

// CreateInterface generates a WireGuard key pair, assigns the next available
// interface ID (wg10, wg11, …) and listen port (51830+), inserts into SQLite,
// writes the initial config file, and returns the new TunnelInterface.
// The interface is NOT started; call StartInterface explicitly.
func (m *Manager) CreateInterface(inp CreateInput) (*TunnelInterface, error) {
	if inp.Name == "" {
		return nil, fmt.Errorf("interface name is required")
	}
	if inp.Protocol == "" {
		inp.Protocol = "wireguard-1.0"
	}
	if inp.Protocol == "amneziawg-2.0" && inp.AWG2 == nil {
		return nil, fmt.Errorf("AWG2 settings are required for amneziawg-2.0 protocol")
	}

	// Key generation uses the protocol-specific binary (wg vs awg).
	syncBin := "wg"
	if inp.Protocol == "amneziawg-2.0" {
		syncBin = "awg"
	}
	keys, err := peer.GenerateKeys(syncBin)
	if err != nil {
		return nil, fmt.Errorf("generate keys: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextInterfaceID()
	port := inp.ListenPort
	if port == 0 {
		port = m.nextListenPort()
	}

	t, err := Create(InterfaceInput{
		ID:            id,
		Name:          inp.Name,
		Protocol:      inp.Protocol,
		Address:       inp.Address,
		ListenPort:    port,
		DisableRoutes: inp.DisableRoutes,
		PrivateKey:    keys.PrivateKey,
		PublicKey:     keys.PublicKey,
		AWG2:          inp.AWG2,
	})
	if err != nil {
		return nil, err
	}

	// Write the initial config file so the first Start() can succeed without errors.
	if err := t.RegenerateConfig(); err != nil {
		log.Printf("tunnel: create %s: regenerate config warning: %v", id, err)
	}

	m.interfaces[id] = t
	log.Printf("tunnel: interface %s created (protocol=%s port=%d)", id, inp.Protocol, port)
	return t, nil
}

// GetInterface returns the TunnelInterface for the given ID, or nil if not found.
func (m *Manager) GetInterface(id string) *TunnelInterface {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.interfaces[id]
}

// GetAllInterfaces returns a snapshot slice of all interfaces in creation order.
// Sorted by CreatedAt ASC — map iteration order is non-deterministic in Go
// (FIX-GO-13 applied at manager level).
func (m *Manager) GetAllInterfaces() []*TunnelInterface {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*TunnelInterface, 0, len(m.interfaces))
	for _, t := range m.interfaces {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

// UpdateInterface applies upd to the interface, persists, regenerates config,
// and hot-reloads via syncconf if the interface is running.
func (m *Manager) UpdateInterface(id string, upd InterfaceUpdate) (*TunnelInterface, error) {
	t := m.GetInterface(id)
	if t == nil {
		return nil, fmt.Errorf("interface %q not found", id)
	}
	return t, t.Update(upd)
}

// DeleteInterface stops the interface, removes all peers and the row from SQLite,
// deletes the config file, and removes it from the in-memory map.
func (m *Manager) DeleteInterface(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := m.interfaces[id]
	if t == nil {
		return fmt.Errorf("interface %q not found", id)
	}

	if err := t.Delete(); err != nil {
		return err
	}
	delete(m.interfaces, id)
	return nil
}

// ── Lifecycle wrappers ────────────────────────────────────────────────────────

// StartInterface starts the interface and returns it.
func (m *Manager) StartInterface(id string) (*TunnelInterface, error) {
	t := m.GetInterface(id)
	if t == nil {
		return nil, fmt.Errorf("interface %q not found", id)
	}
	return t, t.Start()
}

// StopInterface stops the interface and returns it.
func (m *Manager) StopInterface(id string) (*TunnelInterface, error) {
	t := m.GetInterface(id)
	if t == nil {
		return nil, fmt.Errorf("interface %q not found", id)
	}
	return t, t.Stop()
}

// RestartInterface restarts the interface and returns it.
func (m *Manager) RestartInterface(id string) (*TunnelInterface, error) {
	t := m.GetInterface(id)
	if t == nil {
		return nil, fmt.Errorf("interface %q not found", id)
	}
	return t, t.Restart()
}

// ── Peer wrappers ─────────────────────────────────────────────────────────────

// AddPeer adds a peer to the given interface.
func (m *Manager) AddPeer(interfaceID string, inp peer.PeerInput) (*peer.Peer, error) {
	t := m.GetInterface(interfaceID)
	if t == nil {
		return nil, fmt.Errorf("interface %q not found", interfaceID)
	}
	return t.AddPeer(inp)
}

// UpdatePeer updates the peer on the given interface.
func (m *Manager) UpdatePeer(interfaceID, peerID string, upd peer.PeerUpdate) (*peer.Peer, error) {
	t := m.GetInterface(interfaceID)
	if t == nil {
		return nil, fmt.Errorf("interface %q not found", interfaceID)
	}
	return t.UpdatePeer(peerID, upd)
}

// RemovePeer removes the peer from the given interface.
func (m *Manager) RemovePeer(interfaceID, peerID string) error {
	t := m.GetInterface(interfaceID)
	if t == nil {
		return fmt.Errorf("interface %q not found", interfaceID)
	}
	return t.RemovePeer(peerID)
}

// GetPeer returns the in-memory peer from the given interface.
func (m *Manager) GetPeer(interfaceID, peerID string) *peer.Peer {
	t := m.GetInterface(interfaceID)
	if t == nil {
		return nil
	}
	return t.GetPeer(peerID)
}

// GetPeers returns all in-memory peers for the given interface.
func (m *Manager) GetPeers(interfaceID string) ([]*peer.Peer, error) {
	t := m.GetInterface(interfaceID)
	if t == nil {
		return nil, fmt.Errorf("interface %q not found", interfaceID)
	}
	return t.GetAllPeers(), nil
}

// GetAllPeers returns all in-memory peers across all interfaces in stable order.
// Interfaces are sorted by CreatedAt ASC first; within each interface peers are
// already sorted by CreatedAt ASC (FIX-GO-13). Map iteration order is
// non-deterministic — without sorting the dashboard reorders every second.
func (m *Manager) GetAllPeers() []*peer.Peer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ifaces := make([]*TunnelInterface, 0, len(m.interfaces))
	for _, t := range m.interfaces {
		ifaces = append(ifaces, t)
	}
	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].CreatedAt < ifaces[j].CreatedAt
	})
	var out []*peer.Peer
	for _, t := range ifaces {
		out = append(out, t.GetAllPeers()...)
	}
	return out
}

// GetPeerRemoteConfig generates the downloadable WireGuard config for a peer.
// Merges interface data with global settings (DNS, defaultClientAllowedIPs) and
// the WG_HOST public address — matching the JS InterfaceManager.getPeerRemoteConfig().
func (m *Manager) GetPeerRemoteConfig(interfaceID, peerID string) (string, error) {
	t := m.GetInterface(interfaceID)
	if t == nil {
		return "", fmt.Errorf("interface %q not found", interfaceID)
	}

	p := t.GetPeer(peerID)
	if p == nil {
		return "", fmt.Errorf("peer %q not found in interface %q", peerID, interfaceID)
	}

	gs, err := settings.GetSettings()
	if err != nil {
		return "", fmt.Errorf("get settings: %w", err)
	}

	// Build the InterfaceData the peer needs for its [Interface] + [Peer] sections.
	var awg2 *peer.AWG2Settings
	if t.AWG2 != nil {
		cp := *t.AWG2
		awg2 = &cp
	}

	ifaceData := peer.InterfaceData{
		ID:                      t.ID,
		Name:                    t.Name,
		Protocol:                t.Protocol,
		PublicKey:               t.PublicKey,
		Address:                 t.Address,
		ListenPort:              t.ListenPort,
		DNS:                     gs.DNS,
		DefaultClientAllowedIPs: gs.DefaultClientAllowedIPs,
		Host:                    m.WGHost,
		Settings:                awg2,
	}

	return p.GenerateRemoteConfig(ifaceData), nil
}

// ── Private helpers ───────────────────────────────────────────────────────────

// nextInterfaceID returns the lowest available wgN ID starting from wg10.
// Must be called with m.mu held (at least RLock).
func (m *Manager) nextInterfaceID() string {
	for n := 10; ; n++ {
		id := fmt.Sprintf("wg%d", n)
		if _, exists := m.interfaces[id]; !exists {
			return id
		}
	}
}

// nextListenPort returns the lowest available UDP port starting from 51830.
// Must be called with m.mu held (at least RLock).
func (m *Manager) nextListenPort() int {
	used := make(map[int]bool, len(m.interfaces))
	for _, t := range m.interfaces {
		used[t.ListenPort] = true
	}
	for port := 51830; ; port++ {
		if !used[port] {
			return port
		}
	}
}
