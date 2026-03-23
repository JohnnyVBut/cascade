package peer

import (
	"strings"
	"testing"
)

// ── isValidEndpoint ───────────────────────────────────────────────────────────

func TestIsValidEndpoint_Valid(t *testing.T) {
	cases := []string{
		"1.2.3.4:51820",
		"example.com:51820",
		"[::1]:51820",
		"10.0.0.1:65535",
		"my-host.example.org:1234",
	}
	for _, ep := range cases {
		t.Run(ep, func(t *testing.T) {
			if !isValidEndpoint(ep) {
				t.Errorf("isValidEndpoint(%q) = false, want true", ep)
			}
		})
	}
}

func TestIsValidEndpoint_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"no colon", "10.0.0.1"},
		{"colon at start", ":51820"},
		{"non-numeric port", "10.0.0.1:abc"},
		{"empty port", "10.0.0.1:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isValidEndpoint(tc.input) {
				t.Errorf("isValidEndpoint(%q) = true, want false", tc.input)
			}
		})
	}
}

// ── validatePeerInput ─────────────────────────────────────────────────────────

func TestValidatePeerInput_Valid(t *testing.T) {
	inp := PeerInput{
		Name:       "test-peer",
		PublicKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", // 44 chars
		AllowedIPs: "10.8.0.2/32",
	}
	if err := validatePeerInput(inp); err != nil {
		t.Errorf("validatePeerInput valid input returned error: %v", err)
	}
}

func TestValidatePeerInput_EmptyName(t *testing.T) {
	inp := PeerInput{
		Name:       "",
		PublicKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		AllowedIPs: "10.8.0.2/32",
	}
	if err := validatePeerInput(inp); err == nil {
		t.Error("expected error for empty name, got nil")
	}
}

func TestValidatePeerInput_EmptyPublicKey(t *testing.T) {
	inp := PeerInput{
		Name:       "test-peer",
		PublicKey:  "",
		AllowedIPs: "10.8.0.2/32",
	}
	if err := validatePeerInput(inp); err == nil {
		t.Error("expected error for empty public key, got nil")
	}
}

func TestValidatePeerInput_WrongKeyLength(t *testing.T) {
	// 43 chars (not 44)
	inp := PeerInput{
		Name:       "test-peer",
		PublicKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		AllowedIPs: "10.8.0.2/32",
	}
	if err := validatePeerInput(inp); err == nil {
		t.Error("expected error for key != 44 chars, got nil")
	}
}

func TestValidatePeerInput_EmptyAllowedIPs(t *testing.T) {
	inp := PeerInput{
		Name:      "test-peer",
		PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	}
	if err := validatePeerInput(inp); err == nil {
		t.Error("expected error for empty allowedIPs, got nil")
	}
}

func TestValidatePeerInput_BadEndpoint(t *testing.T) {
	inp := PeerInput{
		Name:       "test-peer",
		PublicKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		AllowedIPs: "10.8.0.2/32",
		Endpoint:   "not-valid", // missing port
	}
	if err := validatePeerInput(inp); err == nil {
		t.Error("expected error for bad endpoint, got nil")
	}
}

func TestValidatePeerInput_ValidEndpoint(t *testing.T) {
	inp := PeerInput{
		Name:       "test-peer",
		PublicKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		AllowedIPs: "10.8.0.2/32",
		Endpoint:   "vpn.example.com:51820",
	}
	if err := validatePeerInput(inp); err != nil {
		t.Errorf("validatePeerInput with valid endpoint returned error: %v", err)
	}
}

// ── ToWgConfig ────────────────────────────────────────────────────────────────

func TestToWgConfig_EnabledPeer(t *testing.T) {
	p := &Peer{
		Name:                "Alice",
		PublicKey:           "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		AllowedIPs:          "10.8.0.2/32",
		PresharedKey:        "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB==",
		PersistentKeepalive: 25,
		Enabled:             true,
	}
	cfg := p.ToWgConfig()
	if !strings.Contains(cfg, "[Peer]") {
		t.Error("expected [Peer] section header")
	}
	if !strings.Contains(cfg, "PublicKey = "+p.PublicKey) {
		t.Error("expected PublicKey line")
	}
	if !strings.Contains(cfg, "AllowedIPs = 10.8.0.2/32") {
		t.Error("expected AllowedIPs line")
	}
	if !strings.Contains(cfg, "PresharedKey") {
		t.Error("expected PresharedKey line")
	}
	if !strings.Contains(cfg, "PersistentKeepalive = 25") {
		t.Error("expected PersistentKeepalive line")
	}
}

func TestToWgConfig_DisabledPeer(t *testing.T) {
	p := &Peer{
		Name:       "Disabled",
		PublicKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		AllowedIPs: "10.8.0.3/32",
		Enabled:    false,
	}
	cfg := p.ToWgConfig()
	if cfg != "" {
		t.Errorf("disabled peer should return empty config, got %q", cfg)
	}
}

func TestToWgConfig_NoEndpointWhenEmpty(t *testing.T) {
	p := &Peer{
		Name:       "SansPeer",
		PublicKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		AllowedIPs: "10.8.0.4/32",
		Endpoint:   "",
		Enabled:    true,
	}
	cfg := p.ToWgConfig()
	if strings.Contains(cfg, "Endpoint =") {
		t.Error("should not include Endpoint line when empty")
	}
}

func TestToWgConfig_WithEndpoint(t *testing.T) {
	p := &Peer{
		Name:       "WithEndpoint",
		PublicKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		AllowedIPs: "0.0.0.0/0",
		Endpoint:   "vpn.example.com:51820",
		Enabled:    true,
	}
	cfg := p.ToWgConfig()
	if !strings.Contains(cfg, "Endpoint = vpn.example.com:51820") {
		t.Errorf("expected Endpoint line, got: %s", cfg)
	}
}

// ── generateCompleteConfig ────────────────────────────────────────────────────

func TestGenerateCompleteConfig_WireGuard(t *testing.T) {
	p := &Peer{
		Name:                "client1",
		PrivateKey:          "privatekey123",
		AllowedIPs:          "10.8.0.2/32",
		ClientAllowedIPs:    "0.0.0.0/0",
		PersistentKeepalive: 25,
	}
	iface := InterfaceData{
		Protocol:   "wireguard-1.0",
		PublicKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		Address:    "10.8.0.1/24",
		ListenPort: 51820,
		Host:       "vpn.example.com",
		DNS:        "1.1.1.1",
	}
	cfg := p.generateCompleteConfig(iface)

	if !strings.Contains(cfg, "[Interface]") {
		t.Error("expected [Interface] section")
	}
	if !strings.Contains(cfg, "PrivateKey = privatekey123") {
		t.Error("expected PrivateKey line")
	}
	if !strings.Contains(cfg, "Address = 10.8.0.2/24") {
		t.Error("expected derived Address from AllowedIPs + iface mask")
	}
	if !strings.Contains(cfg, "DNS = 1.1.1.1") {
		t.Error("expected DNS line")
	}
	if !strings.Contains(cfg, "[Peer]") {
		t.Error("expected [Peer] section")
	}
	if !strings.Contains(cfg, "Endpoint = vpn.example.com:51820") {
		t.Error("expected Endpoint line")
	}
	// AWG params should NOT appear for WireGuard 1.0
	if strings.Contains(cfg, "Jc = ") {
		t.Error("unexpected AWG params in WireGuard 1.0 config")
	}
}

func TestGenerateCompleteConfig_AWG2WithSettings(t *testing.T) {
	p := &Peer{
		Name:                "awg-client",
		PrivateKey:          "privatekey456",
		Address:             "10.9.0.2/24",
		ClientAllowedIPs:    "0.0.0.0/0",
		PersistentKeepalive: 25,
	}
	iface := InterfaceData{
		Protocol:  "amneziawg-2.0",
		PublicKey: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		Address:   "10.9.0.1/24",
		Host:      "awg.example.com",
		ListenPort: 51821,
		Settings: &AWG2Settings{
			Jc: 6, Jmin: 64, Jmax: 1280,
			S1: 32, S2: 33, S3: 20, S4: 8,
			H1: "100000000-150000000", H2: "1200000000-1250000000",
			H3: "2400000000-2450000000", H4: "3600000000-3650000000",
			I1: "<r 100>",
		},
	}
	cfg := p.generateCompleteConfig(iface)

	if !strings.Contains(cfg, "Jc = 6") {
		t.Error("expected Jc line in AWG2 config")
	}
	if !strings.Contains(cfg, "H1 = 100000000-150000000") {
		t.Error("expected H1 line in AWG2 config")
	}
	if !strings.Contains(cfg, "I1 = <r 100>") {
		t.Error("expected I1 line in AWG2 config")
	}
}

func TestGenerateCompleteConfig_AddressFromStoredField(t *testing.T) {
	p := &Peer{
		PrivateKey:          "pk",
		Address:             "10.8.0.5/24", // stored address takes precedence
		AllowedIPs:          "10.8.0.5/32",
		ClientAllowedIPs:    "0.0.0.0/0",
		PersistentKeepalive: 25,
	}
	iface := InterfaceData{
		Protocol:  "wireguard-1.0",
		PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		Address:   "10.8.0.1/24",
	}
	cfg := p.generateCompleteConfig(iface)

	if !strings.Contains(cfg, "Address = 10.8.0.5/24") {
		t.Errorf("expected stored address '10.8.0.5/24' in config:\n%s", cfg)
	}
}

func TestGenerateCompleteConfig_DefaultDNS(t *testing.T) {
	p := &Peer{
		PrivateKey:          "pk",
		AllowedIPs:          "10.8.0.2/32",
		ClientAllowedIPs:    "0.0.0.0/0",
		PersistentKeepalive: 25,
	}
	iface := InterfaceData{
		Protocol:  "wireguard-1.0",
		PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		DNS:       "", // empty — should fall back to default
	}
	cfg := p.generateCompleteConfig(iface)

	if !strings.Contains(cfg, "DNS = 1.1.1.1, 8.8.8.8") {
		t.Errorf("expected default DNS fallback in config:\n%s", cfg)
	}
}

// ── GenerateRemoteConfig dispatches correctly ─────────────────────────────────

func TestGenerateRemoteConfig_WithPrivateKey(t *testing.T) {
	p := &Peer{
		PrivateKey:          "myprivatekey",
		AllowedIPs:          "10.8.0.2/32",
		ClientAllowedIPs:    "0.0.0.0/0",
		PersistentKeepalive: 25,
	}
	iface := InterfaceData{
		Protocol:  "wireguard-1.0",
		PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	}
	cfg := p.GenerateRemoteConfig(iface)
	// Complete config has PrivateKey line.
	if !strings.Contains(cfg, "PrivateKey = myprivatekey") {
		t.Errorf("expected real PrivateKey in complete config:\n%s", cfg)
	}
}

func TestGenerateRemoteConfig_WithoutPrivateKey(t *testing.T) {
	p := &Peer{
		Name:                "manual-peer",
		PrivateKey:          "", // empty — template config
		AllowedIPs:          "10.8.0.2/32",
		ClientAllowedIPs:    "0.0.0.0/0",
		PersistentKeepalive: 25,
	}
	iface := InterfaceData{
		Protocol:  "wireguard-1.0",
		PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	}
	cfg := p.GenerateRemoteConfig(iface)
	// Template config has instructional text.
	if !strings.Contains(cfg, "YOUR_PRIVATE_KEY") {
		t.Errorf("expected template placeholder in config:\n%s", cfg)
	}
}

// ── GenerateQRSVG ─────────────────────────────────────────────────────────────

func TestGenerateQRSVG_ProducesSVG(t *testing.T) {
	svg, err := GenerateQRSVG("[Interface]\nPrivateKey = test\n")
	if err != nil {
		t.Fatalf("GenerateQRSVG: %v", err)
	}
	if !strings.HasPrefix(svg, "<svg") {
		t.Errorf("expected SVG starting with '<svg', got: %.50s", svg)
	}
	if !strings.HasSuffix(svg, "</svg>") {
		t.Errorf("expected SVG ending with '</svg>', got: ...%.20s", svg[len(svg)-20:])
	}
}
