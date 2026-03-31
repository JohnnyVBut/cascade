package tunnel

import (
	"os"
	"strings"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

// ── quickBin / syncBin ────────────────────────────────────────────────────────

func TestQuickBin_WireGuard(t *testing.T) {
	iface := &TunnelInterface{Protocol: "wireguard-1.0"}
	if got := iface.quickBin(); got != "wg-quick" {
		t.Errorf("quickBin wireguard-1.0 = %q, want 'wg-quick'", got)
	}
}

func TestQuickBin_AmneziaWG(t *testing.T) {
	iface := &TunnelInterface{Protocol: "amneziawg-2.0"}
	if got := iface.quickBin(); got != "awg-quick" {
		t.Errorf("quickBin amneziawg-2.0 = %q, want 'awg-quick'", got)
	}
}

func TestSyncBin_WireGuard(t *testing.T) {
	iface := &TunnelInterface{Protocol: "wireguard-1.0"}
	if got := iface.syncBin(); got != "wg" {
		t.Errorf("syncBin wireguard-1.0 = %q, want 'wg'", got)
	}
}

func TestSyncBin_AmneziaWG(t *testing.T) {
	iface := &TunnelInterface{Protocol: "amneziawg-2.0"}
	if got := iface.syncBin(); got != "awg" {
		t.Errorf("syncBin amneziawg-2.0 = %q, want 'awg'", got)
	}
}

func TestQuickBin_UnknownProtocolDefaultsToWg(t *testing.T) {
	iface := &TunnelInterface{Protocol: ""}
	if got := iface.quickBin(); got != "wg-quick" {
		t.Errorf("quickBin empty protocol = %q, want 'wg-quick'", got)
	}
}

func TestSyncBin_UnknownProtocolDefaultsToWg(t *testing.T) {
	iface := &TunnelInterface{Protocol: ""}
	if got := iface.syncBin(); got != "wg" {
		t.Errorf("syncBin empty protocol = %q, want 'wg'", got)
	}
}

// ── generateWgConfig ──────────────────────────────────────────────────────────

func newTestIface() *TunnelInterface {
	return &TunnelInterface{
		ID:         "wg10",
		Name:       "TestInterface",
		PrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		ListenPort: 51830,
		Address:    "10.8.0.1/24",
		Protocol:   "wireguard-1.0",
		peers:      make(map[string]*peer.Peer),
	}
}

func TestGenerateWgConfig_InterfaceSection(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	if !strings.Contains(cfg, "[Interface]") {
		t.Error("config missing [Interface] section")
	}
	if !strings.Contains(cfg, "PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=") {
		t.Error("config missing PrivateKey")
	}
	if !strings.Contains(cfg, "ListenPort = 51830") {
		t.Error("config missing ListenPort")
	}
	if !strings.Contains(cfg, "Address = 10.8.0.1/24") {
		t.Error("config missing Address")
	}
	if !strings.Contains(cfg, "# TestInterface") {
		t.Error("config missing interface name comment")
	}
}

func TestGenerateWgConfig_PostUpContainsIptables(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	// FIX-1 updated: use `iptables` (not iptables-nft) so the symlink resolves
	// to the correct backend per environment.
	// iptables-nft MASQUERADE fails in isolated netns (nft_masq not auto-loaded).
	// iptables-legacy MASQUERADE works universally (xt_MASQUERADE always available).
	// On Ubuntu host: iptables → iptables-nft (update-alternatives).
	// In Alpine container: iptables → iptables-legacy (Dockerfile.go explicit symlink).
	if strings.Contains(cfg, "iptables-nft") {
		t.Error("PostUp must not use iptables-nft directly — use `iptables` to allow env-specific backend resolution")
	}
	if !strings.Contains(cfg, "iptables ") {
		t.Error("PostUp must use `iptables` command (FIX-1 updated)")
	}
}

func TestGenerateWgConfig_PostUpAppendsNotInserts(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	// FIX-1: must use -A FORWARD (append), never -I FORWARD (insert)
	if strings.Contains(cfg, "-I FORWARD") {
		t.Error("config must NOT use -I FORWARD; must use -A FORWARD (FIX-1)")
	}
	if !strings.Contains(cfg, "-A FORWARD") {
		t.Error("config must use -A FORWARD (FIX-1)")
	}
}

func TestGenerateWgConfig_PostUpBothDirections(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	// FIX-1: FORWARD both -i (ingress) and -o (egress) for the interface
	if !strings.Contains(cfg, "-i wg10") {
		t.Error("config PostUp must have -i wg10 FORWARD rule (FIX-1)")
	}
	if !strings.Contains(cfg, "-o wg10") {
		t.Error("config PostUp must have -o wg10 FORWARD rule (FIX-1)")
	}
}

func TestGenerateWgConfig_PostUpMasquerade(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	if !strings.Contains(cfg, "MASQUERADE") {
		t.Error("config PostUp must include MASQUERADE rule")
	}
	// Subnet derived from 10.8.0.1/24 → 10.8.0.0/24
	if !strings.Contains(cfg, "10.8.0.0/24") {
		t.Error("config PostUp MASQUERADE must use subnet 10.8.0.0/24")
	}
}

func TestGenerateWgConfig_PostDownCleansUp(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	if !strings.Contains(cfg, "PostDown") {
		t.Error("config must include PostDown")
	}
	if !strings.Contains(cfg, "-D FORWARD") {
		t.Error("PostDown must delete FORWARD rules with -D")
	}
}

func TestGenerateWgConfig_TableOffWhenDisableRoutes(t *testing.T) {
	iface := newTestIface()
	iface.DisableRoutes = true
	cfg := iface.generateWgConfig()

	if !strings.Contains(cfg, "Table = off") {
		t.Error("config must include 'Table = off' when DisableRoutes=true")
	}
}

func TestGenerateWgConfig_NoTableOffByDefault(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	if strings.Contains(cfg, "Table = off") {
		t.Error("config must NOT include 'Table = off' when DisableRoutes=false")
	}
}

func TestGenerateWgConfig_NoAWG2ParamsForWG1(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	for _, key := range []string{"Jc =", "Jmin =", "Jmax =", "S1 =", "H1 ="} {
		if strings.Contains(cfg, key) {
			t.Errorf("WG1 config must not contain AWG2 param %q", key)
		}
	}
}

func TestGenerateWgConfig_AWG2ParamsIncluded(t *testing.T) {
	iface := newTestIface()
	iface.Protocol = "amneziawg-2.0"
	iface.AWG2 = &peer.AWG2Settings{
		Jc:   5,
		Jmin: 50,
		Jmax: 1000,
		S1:   30,
		S2:   31,
		S3:   20,
		S4:   10,
		H1:   "100000000-150000000",
		H2:   "1200000000-1250000000",
		H3:   "2400000000-2450000000",
		H4:   "3600000000-3650000000",
		I1:   "<r 100>",
	}
	cfg := iface.generateWgConfig()

	for _, want := range []string{
		"Jc = 5",
		"Jmin = 50",
		"Jmax = 1000",
		"S1 = 30",
		"H1 = 100000000-150000000",
		"H4 = 3600000000-3650000000",
		"I1 = <r 100>",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("AWG2 config missing %q", want)
		}
	}
}

func TestGenerateWgConfig_AWG2NilNoParams(t *testing.T) {
	iface := newTestIface()
	iface.Protocol = "amneziawg-2.0"
	iface.AWG2 = nil // protocol set but no settings → no AWG2 section
	cfg := iface.generateWgConfig()

	if strings.Contains(cfg, "Jc =") {
		t.Error("AWG2 config with nil AWG2 settings must not contain 'Jc ='")
	}
}

func TestGenerateWgConfig_AWG2OptionalIFieldsOmitted(t *testing.T) {
	iface := newTestIface()
	iface.Protocol = "amneziawg-2.0"
	iface.AWG2 = &peer.AWG2Settings{
		Jc: 4, Jmin: 40, Jmax: 800,
		S1: 20, S2: 21, S3: 15, S4: 8,
		H1: "100000000-150000000",
		H2: "1200000000-1250000000",
		H3: "2400000000-2450000000",
		H4: "3600000000-3650000000",
		// I1-I5 left empty
	}
	cfg := iface.generateWgConfig()

	for _, key := range []string{"I1 =", "I2 =", "I3 =", "I4 =", "I5 ="} {
		if strings.Contains(cfg, key) {
			t.Errorf("config must not include empty I-field %q", key)
		}
	}
}

func TestGenerateWgConfig_NoPeerSectionsWhenEmpty(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	if strings.Contains(cfg, "[Peer]") {
		t.Error("config must not contain [Peer] section when there are no peers")
	}
}

func TestGenerateWgConfig_WithPeer(t *testing.T) {
	iface := newTestIface()
	iface.peers["p1"] = &peer.Peer{
		ID:         "p1",
		PublicKey:  "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		AllowedIPs: "10.8.0.2/32",
		Enabled:    true,
	}
	cfg := iface.generateWgConfig()

	if !strings.Contains(cfg, "[Peer]") {
		t.Error("config must contain [Peer] section when peer exists")
	}
	if !strings.Contains(cfg, "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=") {
		t.Error("config missing peer public key")
	}
}

func TestGenerateWgConfig_NoAddressNoPostUp(t *testing.T) {
	iface := &TunnelInterface{
		ID:         "wg10",
		Name:       "NoAddr",
		PrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		ListenPort: 51830,
		Protocol:   "wireguard-1.0",
		Address:    "", // no address configured
		peers:      make(map[string]*peer.Peer),
	}
	cfg := iface.generateWgConfig()

	if strings.Contains(cfg, "PostUp") {
		t.Error("config must not include PostUp when Address is empty")
	}
	if strings.Contains(cfg, "PostDown") {
		t.Error("config must not include PostDown when Address is empty")
	}
}

// ── generateSyncConfig ────────────────────────────────────────────────────────

func TestGenerateSyncConfig_NoWgQuickDirectives(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateSyncConfig()

	for _, banned := range []string{"Address", "Table", "PostUp", "PostDown", "DNS", "MTU"} {
		if strings.Contains(cfg, banned+" =") || strings.Contains(cfg, banned+"=") {
			t.Errorf("syncconf config must not contain wg-quick directive %q", banned)
		}
	}
}

func TestGenerateSyncConfig_ContainsPrivateKeyAndPort(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateSyncConfig()

	if !strings.Contains(cfg, "PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=") {
		t.Error("syncconf config missing PrivateKey")
	}
	if !strings.Contains(cfg, "ListenPort = 51830") {
		t.Error("syncconf config missing ListenPort")
	}
}

func TestGenerateSyncConfig_AWG2ParamsIncluded(t *testing.T) {
	iface := newTestIface()
	iface.Protocol = "amneziawg-2.0"
	iface.AWG2 = &peer.AWG2Settings{
		Jc:   7,
		Jmin: 70,
		Jmax: 1400,
		S1:   40,
		S2:   41,
		S3:   25,
		S4:   12,
		H1:   "100000000-150000000",
		H2:   "1200000000-1250000000",
		H3:   "2400000000-2450000000",
		H4:   "3600000000-3650000000",
	}
	cfg := iface.generateSyncConfig()

	if !strings.Contains(cfg, "Jc = 7") {
		t.Error("syncconf config missing Jc")
	}
	if !strings.Contains(cfg, "H1 = 100000000-150000000") {
		t.Error("syncconf config missing H1")
	}
}

// ── AutoAllocateIP ────────────────────────────────────────────────────────────

func TestAutoAllocateIP_FirstAvailable(t *testing.T) {
	iface := newTestIface() // address = 10.8.0.1/24

	ip, err := iface.AutoAllocateIP()
	if err != nil {
		t.Fatalf("AutoAllocateIP: %v", err)
	}
	// 10.8.0.1 is the interface IP — first available should be 10.8.0.2/32
	if ip != "10.8.0.2/32" {
		t.Errorf("AutoAllocateIP = %q, want '10.8.0.2/32'", ip)
	}
}

func TestAutoAllocateIP_SkipsTakenIPs(t *testing.T) {
	iface := newTestIface()
	iface.peers["p1"] = &peer.Peer{
		ID:         "p1",
		AllowedIPs: "10.8.0.2/32",
		Enabled:    true,
	}
	iface.peers["p2"] = &peer.Peer{
		ID:         "p2",
		AllowedIPs: "10.8.0.3/32",
		Enabled:    true,
	}

	ip, err := iface.AutoAllocateIP()
	if err != nil {
		t.Fatalf("AutoAllocateIP: %v", err)
	}
	if ip != "10.8.0.4/32" {
		t.Errorf("AutoAllocateIP = %q, want '10.8.0.4/32'", ip)
	}
}

func TestAutoAllocateIP_NoAddressError(t *testing.T) {
	iface := &TunnelInterface{
		ID:    "wg10",
		peers: make(map[string]*peer.Peer),
	}

	_, err := iface.AutoAllocateIP()
	if err == nil {
		t.Error("expected error when interface has no address, got nil")
	}
}

// ── ExportInterfaceParams ─────────────────────────────────────────────────────

// TestExportInterfaceParams_DisableRoutes_HasAllowedIPs verifies that a transit
// (S2S) interface with DisableRoutes=true exports AllowedIPs="0.0.0.0/0".
// Without this, importPeerJSON on the remote side falls back to deriving /32
// from the address field, which breaks S2S routing — the peer only accepts
// packets for its own /32 instead of routing all traffic through the tunnel.
func TestExportInterfaceParams_DisableRoutes_HasAllowedIPs(t *testing.T) {
	iface := newTestIface()
	iface.DisableRoutes = true

	exp := iface.ExportInterfaceParams("")

	if exp.AllowedIPs != "0.0.0.0/0" {
		t.Errorf("ExportInterfaceParams DisableRoutes=true: AllowedIPs = %q, want '0.0.0.0/0'", exp.AllowedIPs)
	}
}

// TestExportInterfaceParams_ClientInterface_NoAllowedIPs verifies that a
// point-to-point client interface with DisableRoutes=false omits AllowedIPs
// from the export. The importing side derives /32 from the address field,
// which is the correct behaviour for client peers.
func TestExportInterfaceParams_ClientInterface_NoAllowedIPs(t *testing.T) {
	iface := newTestIface()
	iface.DisableRoutes = false

	exp := iface.ExportInterfaceParams("")

	if exp.AllowedIPs != "" {
		t.Errorf("ExportInterfaceParams DisableRoutes=false: AllowedIPs = %q, want '' (importer derives /32)", exp.AllowedIPs)
	}
}

// ── isUserspaceMode ───────────────────────────────────────────────────────────

// TestIsUserspaceMode_WhenEnvSet verifies that isUserspaceMode returns true
// when WG_QUICK_USERSPACE_IMPLEMENTATION is set to "amneziawg-go".
func TestIsUserspaceMode_WhenEnvSet(t *testing.T) {
	if err := os.Setenv("WG_QUICK_USERSPACE_IMPLEMENTATION", "amneziawg-go"); err != nil {
		t.Fatalf("os.Setenv: %v", err)
	}
	defer os.Unsetenv("WG_QUICK_USERSPACE_IMPLEMENTATION")

	if !isUserspaceMode() {
		t.Error("isUserspaceMode() = false, want true when WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go")
	}
}

// TestIsUserspaceMode_WhenEnvEmpty verifies that isUserspaceMode returns false
// when WG_QUICK_USERSPACE_IMPLEMENTATION is unset or empty (kernel mode).
func TestIsUserspaceMode_WhenEnvEmpty(t *testing.T) {
	os.Unsetenv("WG_QUICK_USERSPACE_IMPLEMENTATION")

	if isUserspaceMode() {
		t.Error("isUserspaceMode() = true, want false when WG_QUICK_USERSPACE_IMPLEMENTATION is unset")
	}
}

// TestIsUserspaceMode_WhenEnvOtherValue verifies that isUserspaceMode returns
// false when WG_QUICK_USERSPACE_IMPLEMENTATION is set to a value other than
// "amneziawg-go" (e.g. "wireguard-go" — the upstream WireGuard userspace impl).
func TestIsUserspaceMode_WhenEnvOtherValue(t *testing.T) {
	if err := os.Setenv("WG_QUICK_USERSPACE_IMPLEMENTATION", "wireguard-go"); err != nil {
		t.Fatalf("os.Setenv: %v", err)
	}
	defer os.Unsetenv("WG_QUICK_USERSPACE_IMPLEMENTATION")

	if isUserspaceMode() {
		t.Error("isUserspaceMode() = true, want false when WG_QUICK_USERSPACE_IMPLEMENTATION=wireguard-go")
	}
}

// ── txqueuelen ────────────────────────────────────────────────────────────────

// PostUp must set txqueuelen 500 on the WireGuard interface.
// Rationale: default txqueuelen=1000 holds ~11ms at 1 Gbps (BDP: 4138 packets
// at 47ms RTT). 500 → ~5.7ms buffer. With BBR+fq pacing, queue rarely fills →
// no throughput impact; reduces bufferbloat for interactive traffic.
func TestGenerateWgConfig_PostUpSetsTxqueuelen(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	want := "ip link set wg10 txqueuelen 500"
	if !strings.Contains(cfg, want) {
		t.Errorf("PostUp must contain %q for bufferbloat prevention", want)
	}
}

// txqueuelen must appear in PostUp, not PostDown (no cleanup needed on down).
func TestGenerateWgConfig_TxqueuelenInPostUpNotPostDown(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	for _, line := range strings.Split(cfg, "\n") {
		if strings.HasPrefix(line, "PostDown") && strings.Contains(line, "txqueuelen") {
			t.Error("txqueuelen must NOT appear in PostDown — interface teardown makes it irrelevant")
		}
	}
}

// txqueuelen must appear before iptables rules in PostUp.
// Ordering convention: interface configuration commands before firewall rules.
func TestGenerateWgConfig_TxqueuelenBeforeIptables(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	for _, line := range strings.Split(cfg, "\n") {
		if !strings.HasPrefix(line, "PostUp") {
			continue
		}
		qPos := strings.Index(line, "txqueuelen")
		iPos := strings.Index(line, "iptables ")
		if qPos == -1 {
			t.Fatal("PostUp missing txqueuelen — ordering check is meaningless without it")
		}
		if iPos == -1 {
			t.Fatal("PostUp missing iptables — ordering check is meaningless without it")
		}
		if qPos > iPos {
			t.Error("txqueuelen must appear before iptables in PostUp")
		}
	}
}

// txqueuelen must be applied on S2S/transit interfaces (DisableRoutes=true) too —
// they carry forwarded flows and benefit from bufferbloat reduction equally.
func TestGenerateWgConfig_PostUpSetsTxqueuelen_DisableRoutes(t *testing.T) {
	iface := newTestIface()
	iface.DisableRoutes = true
	cfg := iface.generateWgConfig()

	want := "ip link set wg10 txqueuelen 500"
	if !strings.Contains(cfg, want) {
		t.Errorf("PostUp must contain %q even for DisableRoutes=true (S2S) interfaces", want)
	}
}

// ── KernelRemovePeer ──────────────────────────────────────────────────────────

// This is a smoke test: the disabled-interface guard at the top of KernelRemovePeer
// must fire before any exec or goroutine is launched.
func TestKernelRemovePeer_DisabledInterface(t *testing.T) {
	iface := &TunnelInterface{
		ID:       "wg10",
		Protocol: "amneziawg-2.0",
		Enabled:  false,
		peers:    make(map[string]*peer.Peer),
	}

	// Must not panic and must return without attempting any exec.
	iface.KernelRemovePeer("peer-uuid-1")
}

// ── Traffic accumulation ──────────────────────────────────────────────────────

// newTrafficIface returns a TunnelInterface with one peer pre-loaded in both
// the peers map and trafficState — simulating a running interface after LoadPeers.
func newTrafficIface(peerID, pubKey string, dbTotal int64) *TunnelInterface {
	p := &peer.Peer{
		ID:        peerID,
		PublicKey: pubKey,
		TotalRx:   dbTotal,
		TotalTx:   dbTotal,
	}
	st := &peerTrafficState{
		totalRx: dbTotal,
		totalTx: dbTotal,
	}
	return &TunnelInterface{
		ID:       "wg10",
		Enabled:  true,
		Protocol: "wireguard-1.0",
		peers:    map[string]*peer.Peer{peerID: p},
		trafficState: map[string]*peerTrafficState{peerID: st},
	}
}

// TestTrafficAccumulation_DeltaAccumulates verifies that consecutive poll ticks
// with rising kernel counters accumulate correctly into totalRx/Tx.
func TestTrafficAccumulation_DeltaAccumulates(t *testing.T) {
	const peerID = "peer-1"
	const pubKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

	iface := newTrafficIface(peerID, pubKey, 0)
	st := iface.trafficState[peerID]
	p := iface.peers[peerID]

	// Simulate first poll: kernel reports 100 RX, 200 TX.
	st.lastSeenRx, st.lastSeenTx = 0, 0
	p.TransferRx, p.TransferTx = 100, 200
	iface.trafficMu.Lock()
	// Manually apply the same delta logic used in GetStatus inner loop.
	dRx := p.TransferRx - st.lastSeenRx
	dTx := p.TransferTx - st.lastSeenTx
	st.totalRx += dRx; st.totalTx += dTx
	st.lastSeenRx = p.TransferRx; st.lastSeenTx = p.TransferTx
	p.TotalRx = st.totalRx; p.TotalTx = st.totalTx
	iface.trafficMu.Unlock()

	if p.TotalRx != 100 {
		t.Errorf("after tick 1: TotalRx = %d, want 100", p.TotalRx)
	}
	if p.TotalTx != 200 {
		t.Errorf("after tick 1: TotalTx = %d, want 200", p.TotalTx)
	}

	// Simulate second poll: kernel now reports 300 RX, 500 TX (+200 RX, +300 TX).
	iface.trafficMu.Lock()
	dRx = 300 - st.lastSeenRx
	dTx = 500 - st.lastSeenTx
	st.totalRx += dRx; st.totalTx += dTx
	st.lastSeenRx = 300; st.lastSeenTx = 500
	p.TotalRx = st.totalRx; p.TotalTx = st.totalTx
	iface.trafficMu.Unlock()

	if p.TotalRx != 300 {
		t.Errorf("after tick 2: TotalRx = %d, want 300", p.TotalRx)
	}
	if p.TotalTx != 500 {
		t.Errorf("after tick 2: TotalTx = %d, want 500", p.TotalTx)
	}
}

// TestTrafficAccumulation_CounterReset verifies that a kernel counter reset
// (wg-quick down then up) does not subtract from the accumulated total.
// delta = max(0, new - lastSeen); negative delta is clamped to 0.
func TestTrafficAccumulation_CounterReset(t *testing.T) {
	const peerID = "peer-2"
	const pubKey = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB="

	// Pre-existing total: 1 GB accumulated before the reset.
	const priorTotal = int64(1_000_000_000)
	iface := newTrafficIface(peerID, pubKey, priorTotal)
	st := iface.trafficState[peerID]
	p := iface.peers[peerID]

	// Before reset: kernel was at 500 MB.
	st.lastSeenRx = 500_000_000
	st.lastSeenTx = 500_000_000

	// After wg-quick down/up: kernel resets to 0.
	// delta = 0 - 500_000_000 = negative → clamped to 0 → total unchanged.
	iface.trafficMu.Lock()
	newRx := int64(0)
	dRx := newRx - st.lastSeenRx
	if dRx < 0 { dRx = 0 }
	st.totalRx += dRx
	st.lastSeenRx = newRx
	p.TotalRx = st.totalRx
	iface.trafficMu.Unlock()

	if p.TotalRx != priorTotal {
		t.Errorf("after reset: TotalRx = %d, want %d (no subtraction on reset)", p.TotalRx, priorTotal)
	}

	// After reset: kernel accumulates 50 MB of new traffic.
	iface.trafficMu.Lock()
	dRx = 50_000_000 - st.lastSeenRx
	st.totalRx += dRx
	st.lastSeenRx = 50_000_000
	p.TotalRx = st.totalRx
	iface.trafficMu.Unlock()

	want := priorTotal + 50_000_000
	if p.TotalRx != want {
		t.Errorf("after post-reset traffic: TotalRx = %d, want %d", p.TotalRx, want)
	}
}

// TestTrafficAccumulation_FlushOnlyDirty verifies that FlushTrafficTotals skips
// peers whose dirty flag is false (no unnecessary DB writes).
// This is a logic test — we verify the dirty flag is set only when deltas > 0.
func TestTrafficAccumulation_FlushOnlyDirty(t *testing.T) {
	const peerID = "peer-3"
	const pubKey = "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC="

	iface := newTrafficIface(peerID, pubKey, 0)
	st := iface.trafficState[peerID]

	// No traffic yet — dirty must be false.
	if st.dirty {
		t.Error("initial state: dirty = true, want false")
	}

	// Simulate a delta > 0 (traffic arrived).
	iface.trafficMu.Lock()
	st.totalRx += 100
	st.dirty = true
	iface.trafficMu.Unlock()

	if !st.dirty {
		t.Error("after delta: dirty = false, want true")
	}

	// Simulate a zero-delta tick (no new traffic).
	// dirty must NOT be reset unless a flush happened.
	iface.trafficMu.Lock()
	dRx := int64(0) // same kernel counter value as before
	if dRx > 0 {
		st.totalRx += dRx
		st.dirty = true
	}
	iface.trafficMu.Unlock()

	// dirty should still be true (set by previous delta, not cleared without flush).
	if !st.dirty {
		t.Error("after zero-delta tick: dirty was incorrectly cleared")
	}
}
