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
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JohnnyVBut/cascade/internal/awgparams"
	"github.com/JohnnyVBut/cascade/internal/peer"
	"github.com/JohnnyVBut/cascade/internal/settings"
	"github.com/JohnnyVBut/cascade/internal/tc"
	"github.com/JohnnyVBut/cascade/internal/validate"
)

// Manager manages the collection of all TunnelInterface instances.
type Manager struct {
	mu         sync.RWMutex
	interfaces map[string]*TunnelInterface

	stopCh chan struct{} // closed by Stop() to signal the polling goroutine to exit
	doneCh chan struct{} // closed by the polling goroutine after final flush completes

	WGHost string // WG_HOST value — used in ExportInterfaceParams calls
}

// CreateInput is the payload for Manager.CreateInterface.
type CreateInput struct {
	Name          string
	Protocol      string             // default: "wireguard-1.0"
	Address       string             // CIDR e.g. "10.8.0.1/24"
	ListenPort    int                // 0 = auto-assign; if PortPool is also set, pool takes priority
	PortPool      string             // when non-empty and ListenPort==0: select port from pool under lock
	DisableRoutes bool
	AWG2          *peer.AWG2Settings // required for amneziawg-2.0
	DNS           string             // per-interface DNS override; empty = use global
}

// QuickCreateResult is returned by Manager.QuickCreate.
type QuickCreateResult struct {
	Interface  *TunnelInterface
	Started    bool
	StartError error
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
			doneCh:     make(chan struct{}),
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

// Stop signals the polling goroutine to exit and blocks until it has completed
// its final FlushTrafficTotals() call. Safe to call only once.
// Call before db.Close() on graceful shutdown so traffic totals are saved.
func (m *Manager) Stop() {
	close(m.stopCh)
	<-m.doneCh // wait for the goroutine to finish the final flush
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

// startPolling launches a goroutine that:
//   - calls GetStatus on every enabled interface once per second (runtime stats)
//   - flushes dirty traffic totals to SQLite every 60 s (persistence)
//   - performs a final flush on shutdown before returning
//
// Stops when Stop() is called (closes stopCh).
func (m *Manager) startPolling() {
	go func() {
		ticker := time.NewTicker(time.Second)
		flushTicker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		defer flushTicker.Stop()
		for {
			select {
			case <-ticker.C:
				m.mu.RLock()
				for _, t := range m.interfaces {
					t.GetStatus() // updates runtime peer fields; no-op when !t.Enabled
				}
				m.mu.RUnlock()
			case <-flushTicker.C:
				// Periodic flush: max data loss on crash = 60 s of traffic.
				m.mu.RLock()
				for _, t := range m.interfaces {
					t.FlushTrafficTotals()
				}
				m.mu.RUnlock()
			case <-m.stopCh:
				// Final flush before exit (graceful shutdown path).
				// Must complete before Stop() returns so db.Close() is safe.
				m.mu.RLock()
				for _, t := range m.interfaces {
					t.FlushTrafficTotals()
				}
				m.mu.RUnlock()
				close(m.doneCh) // unblocks Stop()
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
// If inp.Name is empty it defaults to the assigned interface ID.
func (m *Manager) CreateInterface(inp CreateInput) (*TunnelInterface, error) {
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

	// Default name to interface ID if not provided.
	name := inp.Name
	if name == "" {
		name = id
	}

	// Port selection happens under the lock to prevent concurrent QuickCreate calls
	// from assigning the same port to two different interfaces.
	port := inp.ListenPort
	if port == 0 {
		if inp.PortPool != "" {
			// Pool-based selection with UDP bind test — runs under the lock so two
			// concurrent QuickCreate calls cannot select the same port.
			var portErr error
			port, portErr = m.nextListenPortFromPoolLocked(inp.PortPool)
			if portErr != nil {
				return nil, fmt.Errorf("select port from pool: %w", portErr)
			}
		} else {
			port = m.nextListenPort()
		}
	}

	t, err := Create(InterfaceInput{
		ID:            id,
		Name:          name,
		Protocol:      inp.Protocol,
		Address:       inp.Address,
		ListenPort:    port,
		DisableRoutes: inp.DisableRoutes,
		DNS:           inp.DNS,
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

// QuickCreate creates and immediately starts a client interface (disableRoutes=false).
// Address is auto-assigned from settings.SubnetPool (/24 block, first host X.X.X.1/24).
// Port is auto-assigned from settings.PortPool with a UDP bind test performed under
// the Manager lock — preventing two concurrent QuickCreate calls from racing to the
// same port.
// For amneziawg-2.0, AWG2 params come from the default template or a random profile.
// Returns QuickCreateResult with StartError set (non-nil) if creation succeeded but start failed.
func (m *Manager) QuickCreate(name, protocol string) (*QuickCreateResult, error) {
	if protocol == "" {
		protocol = "wireguard-1.0"
	}

	gs, err := settings.GetSettings()
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}

	// Auto-assign subnet from pool.  Done outside the lock — nextSubnet takes its
	// own snapshot. Address collisions are benign because the DB unique constraint
	// will catch them, and subnet exhaustion is checked freshly here before calling
	// CreateInterface (which will recheck under the lock for port selection anyway).
	address, err := m.nextSubnet(gs.SubnetPool)
	if err != nil {
		return nil, fmt.Errorf("no available subnet in pool %q: %w", gs.SubnetPool, err)
	}

	// Build AWG2 params before acquiring the lock (may hit the DB / generator).
	var awg2 *peer.AWG2Settings
	if protocol == "amneziawg-2.0" {
		awg2, err = m.buildAWG2Params()
		if err != nil {
			return nil, fmt.Errorf("build AWG2 params: %w", err)
		}
	}

	// Port selection is delegated to CreateInterface via PortPool field so that it
	// happens under the Manager write lock, preventing two concurrent QuickCreate
	// calls from selecting the same port.
	iface, err := m.CreateInterface(CreateInput{
		Name:          name,
		Protocol:      protocol,
		Address:       address,
		PortPool:      gs.PortPool, // port selected under lock inside CreateInterface
		DisableRoutes: false,       // Quick mode is always client interface
		AWG2:          awg2,
	})
	if err != nil {
		return nil, fmt.Errorf("create interface: %w", err)
	}

	// Start the interface — failure is non-fatal (returned separately).
	startErr := iface.Start()
	return &QuickCreateResult{
		Interface:  iface,
		Started:    startErr == nil,
		StartError: startErr,
	}, nil
}

// ImportConfResult is returned by Manager.ImportConf.
type ImportConfResult struct {
	Interface       *TunnelInterface
	Peer            *peer.Peer
	Started         bool
	StartError      error
	// ConflictWarning is set when the imported address subnet overlaps with
	// an existing interface (the address was converted to /32 to avoid conflicts).
	ConflictWarning string
}

// ImportConf parses a WireGuard / AmneziaWG client .conf file and creates:
//  1. A new tunnel interface with the address from [Interface] (forced to /32)
//     and DisableRoutes=true — the server's kernel routing table is not modified.
//  2. An interconnect peer with the [Peer] parameters (PublicKey, Endpoint, AllowedIPs, …).
//
// The interface is immediately started. If start fails the error is returned in
// StartError (non-nil) but the interface and peer are still persisted.
//
// DisableRoutes=true is always set: importing a client conf on a running server
// must not redirect all traffic through the new tunnel (that would break existing
// clients and firewall rules). PBR via Firewall → Rules is the correct mechanism.
func (m *Manager) ImportConf(name, confContent string) (*ImportConfResult, error) {
	parsed, err := ParseWGConf(confContent)
	if err != nil {
		return nil, fmt.Errorf("parse conf: %w", err)
	}
	if parsed.PeerPublicKey == "" {
		return nil, fmt.Errorf("missing PublicKey in [Peer] section (required for uplink mode)")
	}

	// Validate private key before using it in shell commands (prevent injection).
	// Same check AddPeer performs — see newline injection note in interface.go.
	if err := validate.WGKey(parsed.PrivateKey); err != nil {
		return nil, fmt.Errorf("invalid PrivateKey: %w", err)
	}

	// Force address to /32 to avoid subnet routing conflicts.
	address32 := AddressToHost32(parsed.Address)
	var conflictWarning string
	if parsed.Address != "" && parsed.Address != address32 {
		// Check if the original /NN subnet overlaps with existing interfaces.
		m.mu.RLock()
		for _, t := range m.interfaces {
			if subnetsOverlap(t.Address, parsed.Address) {
				conflictWarning = fmt.Sprintf(
					"Address %s overlaps with existing interface %s (%s); using %s instead.",
					parsed.Address, t.ID, t.Address, address32,
				)
				break
			}
		}
		m.mu.RUnlock()
	}

	// Derive public key from private key using the appropriate binary.
	syncBin := "wg"
	if parsed.Protocol == "amneziawg-2.0" {
		syncBin = "awg"
	}
	keys, err := peer.DerivePublicKey(syncBin, parsed.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("derive public key: %w", err)
	}

	iface, err := m.CreateInterface(CreateInput{
		Name:          name,
		Protocol:      parsed.Protocol,
		Address:       address32,
		DisableRoutes: true, // always — do not pollute kernel routing table
		AWG2:          parsed.AWG2,
	})
	if err != nil {
		return nil, fmt.Errorf("create interface: %w", err)
	}

	// Override the auto-generated key pair with the one from the .conf file.
	// Also preserve MTU if specified in the imported config.
	// Mark as uplink: this interface connects OUT to a remote server, not a hub for clients.
	iface.PrivateKey = parsed.PrivateKey
	iface.PublicKey = keys
	iface.Uplink = true
	if parsed.MTU > 0 {
		iface.MTU = parsed.MTU
	}
	if err := iface.save(); err != nil {
		return nil, fmt.Errorf("save interface keys: %w", err)
	}
	if err := iface.RegenerateConfig(); err != nil {
		return nil, fmt.Errorf("regenerate config: %w", err)
	}

	// Add the remote server as an interconnect peer.
	allowedIPs := parsed.PeerAllowedIPs
	if allowedIPs == "" {
		allowedIPs = "0.0.0.0/0"
	}
	keepalive := parsed.PeerKeepalive
	if keepalive == 0 {
		keepalive = 25
	}
	p, err := iface.AddPeer(peer.PeerInput{
		Name:                "upstream",
		PublicKey:           parsed.PeerPublicKey,
		PresharedKey:        parsed.PeerPresharedKey,
		Endpoint:            parsed.PeerEndpoint,
		AllowedIPs:          allowedIPs,
		ClientAllowedIPs:    allowedIPs,
		PeerType:            "interconnect",
		PersistentKeepalive: keepalive,
	})
	if err != nil {
		// Roll back the interface so no orphan is left in the DB.
		_ = m.DeleteInterface(iface.ID)
		return nil, fmt.Errorf("add upstream peer: %w", err)
	}

	startErr := iface.Start()
	return &ImportConfResult{
		Interface:       iface,
		Peer:            p,
		Started:         startErr == nil,
		StartError:      startErr,
		ConflictWarning: conflictWarning,
	}, nil
}

// ImportConfAsServerResult is returned by Manager.ImportConfAsServer.
type ImportConfAsServerResult struct {
	Interface    *TunnelInterface
	PeersCreated int
	PeersFailed  []string
	Started      bool
	StartError   error
}

// ImportConfAsServer parses a WireGuard / AmneziaWG server .conf file and creates
// a normal client-hub interface (DisableRoutes=false, Uplink=false).
// Each [Peer] section in the conf becomes a client peer on the new interface.
// The interface is started immediately; peer-creation errors are collected but
// do not roll back the interface.
func (m *Manager) ImportConfAsServer(name, confContent string) (*ImportConfAsServerResult, error) {
	parsed, err := ParseWGConf(confContent)
	if err != nil {
		return nil, fmt.Errorf("parse conf: %w", err)
	}
	if err := validate.WGKey(parsed.PrivateKey); err != nil {
		return nil, fmt.Errorf("invalid PrivateKey: %w", err)
	}

	listenPort := parsed.ListenPort // may be 0 — manager will auto-assign

	iface, err := m.CreateInterface(CreateInput{
		Name:          name,
		Protocol:      parsed.Protocol,
		Address:       parsed.Address,
		ListenPort:    listenPort,
		DisableRoutes: false,
		AWG2:          parsed.AWG2,
		DNS:           parsed.DNS,
	})
	if err != nil {
		return nil, fmt.Errorf("create interface: %w", err)
	}

	// Override auto-generated key pair with keys from the .conf file.
	syncBin := "wg"
	if parsed.Protocol == "amneziawg-2.0" {
		syncBin = "awg"
	}
	keys, err := peer.DerivePublicKey(syncBin, parsed.PrivateKey)
	if err != nil {
		_ = m.DeleteInterface(iface.ID)
		return nil, fmt.Errorf("derive public key: %w", err)
	}
	iface.PrivateKey = parsed.PrivateKey
	iface.PublicKey = keys
	if parsed.MTU > 0 {
		iface.MTU = parsed.MTU
	}
	if err := iface.save(); err != nil {
		_ = m.DeleteInterface(iface.ID)
		return nil, fmt.Errorf("save interface keys: %w", err)
	}
	if err := iface.RegenerateConfig(); err != nil {
		_ = m.DeleteInterface(iface.ID)
		return nil, fmt.Errorf("regenerate config: %w", err)
	}

	// Import [Peer] sections as client peers.
	var peersCreated int
	var peersFailed []string
	for i, pp := range parsed.Peers {
		if err := validate.WGKey(pp.PublicKey); err != nil {
			peersFailed = append(peersFailed, fmt.Sprintf("peer-%d (invalid key)", i+1))
			continue
		}
		addr := pp.AllowedIPs
		if addr == "" {
			peersFailed = append(peersFailed, fmt.Sprintf("peer-%d (no AllowedIPs)", i+1))
			continue
		}
		// Use the first AllowedIPs entry as the peer address.
		addr = strings.TrimSpace(strings.SplitN(addr, ",", 2)[0])
		name := fmt.Sprintf("peer-%d", i+1)
		_, addErr := iface.AddPeer(peer.PeerInput{
			Name:         name,
			PublicKey:    pp.PublicKey,
			PresharedKey: pp.PresharedKey,
			AllowedIPs:   addr,
			Address:      addr,
			PeerType:     "client",
		})
		if addErr != nil {
			peersFailed = append(peersFailed, fmt.Sprintf("%s: %v", name, addErr))
		} else {
			peersCreated++
		}
	}

	startErr := iface.Start()
	return &ImportConfAsServerResult{
		Interface:    iface,
		PeersCreated: peersCreated,
		PeersFailed:  peersFailed,
		Started:      startErr == nil,
		StartError:   startErr,
	}, nil
}

// subnetsOverlap returns true if two CIDR addresses share any IP range.
func subnetsOverlap(cidr1, cidr2 string) bool {
	_, net1, err1 := net.ParseCIDR(cidr1)
	_, net2, err2 := net.ParseCIDR(cidr2)
	if err1 != nil || err2 != nil {
		return false
	}
	return net1.Contains(net2.IP) || net2.Contains(net1.IP)
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

// StopAll stops all managed WireGuard interfaces (wg-quick down).
// Used before a full restore so kernel state is clean.
func (m *Manager) StopAll() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.interfaces))
	for id := range m.interfaces {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		t := m.GetInterface(id)
		if t == nil {
			continue
		}
		if err := t.Stop(); err != nil {
			log.Printf("tunnel: StopAll: stop %s: %v", id, err)
		}
	}
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

// ReloadPeerFromDB re-reads a single peer from SQLite and refreshes the
// in-memory cache on the given interface. Use after a direct DB write that
// bypasses UpdatePeer (e.g. peer.SavePrivateKey).
func (m *Manager) ReloadPeerFromDB(interfaceID, peerID string) (*peer.Peer, error) {
	t := m.GetInterface(interfaceID)
	if t == nil {
		return nil, fmt.Errorf("interface %q not found", interfaceID)
	}
	return t.ReloadPeerFromDB(peerID)
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

	// Resolve MTU: per-interface override takes priority over global setting.
	mtu := gs.MTU
	if t.MTU > 0 {
		mtu = t.MTU
	}

	ifaceData := peer.InterfaceData{
		ID:                      t.ID,
		Name:                    t.Name,
		Protocol:                t.Protocol,
		PublicKey:               t.PublicKey,
		Address:                 t.Address,
		ListenPort:              t.ListenPort,
		DNS:                     func() string {
			if t.DNS != "" {
				return t.DNS
			}
			return gs.DNS
		}(),
		DefaultClientAllowedIPs: gs.DefaultClientAllowedIPs,
		Host:                    t.resolvedHost(settings.GetWGHost(m.WGHost)),
		Settings:                awg2,
		MTU:                     mtu,
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
// Used by CreateInterface when ListenPort == 0 and no portPool context.
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

// nextListenPortFromPoolLocked finds the first port from portPool that is:
//  1. Not already used by an existing interface.
//  2. Bindable via UDP (net.ListenPacket test).
//
// Must be called with m.mu held (write or read lock) — reads m.interfaces directly
// without acquiring a separate lock.  Used by CreateInterface under m.mu.Lock().
func (m *Manager) nextListenPortFromPoolLocked(portPool string) (int, error) {
	ports, err := settings.ParsePortPool(portPool)
	if err != nil {
		return 0, fmt.Errorf("parse port pool: %w", err)
	}

	used := make(map[int]bool, len(m.interfaces))
	for _, t := range m.interfaces {
		used[t.ListenPort] = true
	}

	for _, p := range ports {
		if used[p] {
			continue
		}
		// UDP bind test — verifies the port is actually free in the OS.
		conn, err := net.ListenPacket("udp", fmt.Sprintf(":%d", p))
		if err != nil {
			continue // port in use by another process
		}
		conn.Close()
		return p, nil
	}
	return 0, fmt.Errorf("all ports in pool are in use")
}

// nextSubnet finds the first /24 block inside pool whose network address is not
// already occupied by an existing interface, and returns "X.X.X.1/24".
//
// Algorithm: enumerate all /24 blocks within the pool CIDR in address order;
// skip any whose network address is already a prefix of an existing interface address.
// Does NOT require m.mu (reads a snapshot).
func (m *Manager) nextSubnet(pool string) (string, error) {
	_, poolNet, err := net.ParseCIDR(pool)
	if err != nil {
		return "", fmt.Errorf("invalid subnet pool CIDR %q: %w", pool, err)
	}

	// Build a set of /24 network addresses already in use.
	m.mu.RLock()
	usedNets := make(map[[4]byte]bool, len(m.interfaces))
	for _, t := range m.interfaces {
		ip, _, parseErr := net.ParseCIDR(t.Address)
		if parseErr != nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		// /24 network address: zero last octet.
		key := [4]byte{ip4[0], ip4[1], ip4[2], 0}
		usedNets[key] = true
	}
	m.mu.RUnlock()

	// Iterate /24 blocks inside the pool.
	base := poolNet.IP.To4()
	if base == nil {
		return "", fmt.Errorf("subnet pool must be an IPv4 CIDR")
	}
	poolOnes, _ := poolNet.Mask.Size()

	// Number of /24 blocks in the pool: 2^(24-poolOnes) if poolOnes <= 24.
	if poolOnes > 24 {
		return "", fmt.Errorf("subnet pool /%d is smaller than /24", poolOnes)
	}

	start := ipToUint32(base)
	// Round start down to a /24 boundary.
	start = start &^ 0xFF

	// Compute the last address in the pool using arithmetic (avoids casting
	// net.IPMask to net.IP, which is semantically fragile).
	poolEnd := start | (uint32(1)<<(32-uint(poolOnes)) - 1)

	for cur := start; cur < poolEnd; cur += 256 {
		curIP := net.IP([]byte{byte(cur >> 24), byte(cur >> 16), byte(cur >> 8), byte(cur)})
		// Check that this /24 is contained in the pool.
		if !poolNet.Contains(curIP) {
			continue
		}
		key := [4]byte{curIP[0], curIP[1], curIP[2], 0}
		if usedNets[key] {
			continue
		}
		// Return the first host (.1) in this /24.
		return fmt.Sprintf("%d.%d.%d.1/24", curIP[0], curIP[1], curIP[2]), nil
	}
	return "", fmt.Errorf("all /24 subnets in pool are in use")
}

// ipToUint32 converts a 4-byte IPv4 address to a uint32 (big-endian).
func ipToUint32(ip net.IP) uint32 {
	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}
	return uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
}

// BuildAWG2Params returns AWG2 params from the default template or a random profile.
// Exported so API handlers can reuse the same logic as QuickCreate.
func (m *Manager) BuildAWG2Params() (*peer.AWG2Settings, error) {
	return m.buildAWG2Params()
}

// buildAWG2Params returns AWG2 params for QuickCreate.
// Priority: default template → random generated profile.
func (m *Manager) buildAWG2Params() (*peer.AWG2Settings, error) {
	p, err := settings.ApplyDefaultTemplate()
	if err != nil {
		return nil, fmt.Errorf("apply default template: %w", err)
	}
	if p != nil {
		return awg2ParamsFromTemplate(p), nil
	}
	// No default template — generate a random profile.
	generated := awgparams.Generate(awgparams.Options{Profile: "random", Intensity: "medium"})
	return awg2ParamsFromGenerated(&generated), nil
}

// awg2ParamsFromTemplate converts settings.AWG2Params to peer.AWG2Settings.
func awg2ParamsFromTemplate(p *settings.AWG2Params) *peer.AWG2Settings {
	return &peer.AWG2Settings{
		Jc:   p.Jc,
		Jmin: p.Jmin,
		Jmax: p.Jmax,
		S1:   p.S1,
		S2:   p.S2,
		S3:   p.S3,
		S4:   p.S4,
		H1:   p.H1,
		H2:   p.H2,
		H3:   p.H3,
		H4:   p.H4,
		I1:   p.I1,
		I2:   p.I2,
		I3:   p.I3,
		I4:   p.I4,
		I5:   p.I5,
	}
}

// awg2ParamsFromGenerated converts awgparams.Params to peer.AWG2Settings.
func awg2ParamsFromGenerated(p *awgparams.Params) *peer.AWG2Settings {
	return &peer.AWG2Settings{
		Jc:   p.Jc,
		Jmin: p.Jmin,
		Jmax: p.Jmax,
		S1:   p.S1,
		S2:   p.S2,
		S3:   p.S3,
		S4:   p.S4,
		H1:   p.H1,
		H2:   p.H2,
		H3:   p.H3,
		H4:   p.H4,
		I1:   p.I1,
		I2:   p.I2,
		I3:   p.I3,
		I4:   p.I4,
		I5:   p.I5,
	}
}

// ApplyGroupTCLimits re-applies rate limits for all client peers in the given group
// across all enabled interfaces. Called when a client-group's rate limits are updated.
// rateDown/rateUp are in kbps; 0 removes the limit.
func (m *Manager) ApplyGroupTCLimits(groupID string, rateDown, rateUp int) {
	m.mu.RLock()
	ifaces := make([]*TunnelInterface, 0, len(m.interfaces))
	for _, t := range m.interfaces {
		ifaces = append(ifaces, t)
	}
	m.mu.RUnlock()

	for _, t := range ifaces {
		if !t.Enabled {
			continue
		}
		t.peersMu.RLock()
		var targets []string
		for _, p := range t.peers {
			if p.PeerType == "client" && p.GroupID == groupID && p.RateDown == 0 && p.RateUp == 0 {
				targets = append(targets, p.AllowedIPs)
			}
		}
		t.peersMu.RUnlock()

		if len(targets) == 0 {
			continue
		}
		if rateDown > 0 || rateUp > 0 {
			tc.EnsureQdisc(t.ID)
		}
		for _, ip := range targets {
			if rateDown > 0 || rateUp > 0 {
				tc.Apply(t.ID, ip, rateDown, rateUp, t.kernelMTU())
			} else {
				tc.Remove(t.ID, ip)
			}
		}
	}
}
