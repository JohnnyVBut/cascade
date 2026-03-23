package tunnel

import (
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

func TestGenerateWgConfig_PostUpContainsIptablesNft(t *testing.T) {
	iface := newTestIface()
	cfg := iface.generateWgConfig()

	// FIX-1: must use iptables-nft, not plain iptables
	if !strings.Contains(cfg, "iptables-nft") {
		t.Error("config PostUp must use iptables-nft (FIX-1)")
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
