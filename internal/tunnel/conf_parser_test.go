package tunnel

import (
	"strings"
	"testing"
)

// validConf is a minimal valid WireGuard client config used as a base in tests.
const validConf = `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.8.0.5/24

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
`

func TestParseWGConf_Valid(t *testing.T) {
	c, err := ParseWGConf(validConf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.PrivateKey != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" {
		t.Errorf("PrivateKey = %q", c.PrivateKey)
	}
	if c.Address != "10.8.0.5/24" {
		t.Errorf("Address = %q", c.Address)
	}
	if c.Protocol != "wireguard-1.0" {
		t.Errorf("Protocol = %q", c.Protocol)
	}
	if c.PeerPublicKey != "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=" {
		t.Errorf("PeerPublicKey = %q", c.PeerPublicKey)
	}
	if c.PeerEndpoint != "vpn.example.com:51820" {
		t.Errorf("PeerEndpoint = %q", c.PeerEndpoint)
	}
	if c.PeerAllowedIPs != "0.0.0.0/0" {
		t.Errorf("PeerAllowedIPs = %q", c.PeerAllowedIPs)
	}
	if c.PeerKeepalive != 25 {
		t.Errorf("PeerKeepalive = %d", c.PeerKeepalive)
	}
}

func TestParseWGConf_WindowsLineEndings(t *testing.T) {
	crlf := strings.ReplaceAll(validConf, "\n", "\r\n")
	c, err := ParseWGConf(crlf)
	if err != nil {
		t.Fatalf("CRLF line endings should be handled: %v", err)
	}
	if c.PeerEndpoint != "vpn.example.com:51820" {
		t.Errorf("PeerEndpoint = %q (CRLF not stripped?)", c.PeerEndpoint)
	}
}

func TestParseWGConf_MissingPrivateKey(t *testing.T) {
	conf := `[Interface]
Address = 10.8.0.5/24

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0
`
	_, err := ParseWGConf(conf)
	if err == nil || !strings.Contains(err.Error(), "PrivateKey") {
		t.Errorf("expected PrivateKey error, got %v", err)
	}
}

func TestParseWGConf_MissingAddress(t *testing.T) {
	conf := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0
`
	_, err := ParseWGConf(conf)
	if err == nil || !strings.Contains(err.Error(), "Address") {
		t.Errorf("expected Address error, got %v", err)
	}
}

func TestParseWGConf_MissingPeerPublicKey(t *testing.T) {
	// A [Peer] without PublicKey is silently skipped (peer with empty key is not added to Peers).
	// Validation that a peer exists is the responsibility of the caller (e.g. ImportConf).
	conf := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.8.0.5/24

[Peer]
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0
`
	c, err := ParseWGConf(conf)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(c.Peers) != 0 {
		t.Errorf("expected no peers (no PublicKey), got %d", len(c.Peers))
	}
	if c.PeerPublicKey != "" {
		t.Errorf("expected empty PeerPublicKey, got %q", c.PeerPublicKey)
	}
}

func TestParseWGConf_TwoPeerSections(t *testing.T) {
	conf := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.8.0.5/24

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = first.example.com:51820
AllowedIPs = 10.0.0.0/8

[Peer]
PublicKey = CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=
Endpoint = second.example.com:51820
AllowedIPs = 192.168.0.0/16
`
	c, err := ParseWGConf(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First peer flat fields should point to the first [Peer].
	if c.PeerPublicKey != "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=" {
		t.Errorf("PeerPublicKey = %q", c.PeerPublicKey)
	}
	if c.PeerEndpoint != "first.example.com:51820" {
		t.Errorf("PeerEndpoint = %q", c.PeerEndpoint)
	}
	// Both peers should be in the Peers slice.
	if len(c.Peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(c.Peers))
	}
	if c.Peers[1].PublicKey != "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=" {
		t.Errorf("second peer PublicKey = %q", c.Peers[1].PublicKey)
	}
}

func TestParseWGConf_MultiLineAllowedIPs(t *testing.T) {
	conf := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.8.0.5/24

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0
AllowedIPs = ::/0
`
	c, err := ParseWGConf(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.PeerAllowedIPs != "0.0.0.0/0, ::/0" {
		t.Errorf("multi-line AllowedIPs not accumulated: %q", c.PeerAllowedIPs)
	}
}

func TestParseWGConf_InlineComments(t *testing.T) {
	conf := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.8.0.5/24

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB= # server key
Endpoint = vpn.example.com:51820 # main server
AllowedIPs = 0.0.0.0/0 # all traffic
`
	c, err := ParseWGConf(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.PeerPublicKey != "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=" {
		t.Errorf("inline comment not stripped from PublicKey: %q", c.PeerPublicKey)
	}
	if c.PeerEndpoint != "vpn.example.com:51820" {
		t.Errorf("inline comment not stripped from Endpoint: %q", c.PeerEndpoint)
	}
	if c.PeerAllowedIPs != "0.0.0.0/0" {
		t.Errorf("inline comment not stripped from AllowedIPs: %q", c.PeerAllowedIPs)
	}
}

func TestParseWGConf_AWG2Detection(t *testing.T) {
	conf := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.9.0.5/24
Jc = 6
Jmin = 64
Jmax = 1280
S1 = 32
S2 = 33
H1 = 100000000-150000000
H2 = 1200000000-1250000000
H3 = 2400000000-2450000000
H4 = 3600000000-3650000000

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = awg.example.com:51821
AllowedIPs = 0.0.0.0/0
`
	c, err := ParseWGConf(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Protocol != "amneziawg-2.0" {
		t.Errorf("AWG2 not detected: Protocol = %q", c.Protocol)
	}
	if c.AWG2 == nil {
		t.Fatal("AWG2 settings should not be nil")
	}
	if c.AWG2.Jc != 6 {
		t.Errorf("Jc = %d, want 6", c.AWG2.Jc)
	}
	if c.AWG2.H1 != "100000000-150000000" {
		t.Errorf("H1 = %q", c.AWG2.H1)
	}
}

// TestParseWGConf_AWG3Detection is a regression test: client .conf exports
// for a v3 interface include Transport Protection fields (HeaderProtectionKey
// etc. — see writeAWG3Fields in internal/peer/peer.go). Before this fix,
// ParseWGConf silently dropped those fields and hardcoded Protocol to
// amneziawg-2.0, so re-importing a v3 client's own .conf as an uplink
// interface would create a v2 interface missing HeaderProtectionKey — the
// resulting handshake against the real v3 server would be silently dropped.
func TestParseWGConf_AWG3Detection(t *testing.T) {
	conf := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.9.0.5/24
Jc = 6
Jmin = 64
Jmax = 1280
S1 = 32
S2 = 33
S3 = 12
S4 = 12
H1 = 100000000-150000000
H2 = 1200000000-1250000000
H3 = 2400000000-2450000000
H4 = 3600000000-3650000000
HeaderProtectionKey = YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=
ContentPaddingAddition = 50-100
RekeyAfterTime = 100-115
RekeyTimeout = 4-6
RejectAfterTime = 140-165
KeepaliveTimeout = 8-12
MaxHandshakeAttempts = 15-25
RandomTrailers = on
DisableCookies = on

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = awg3.example.com:51822
AllowedIPs = 0.0.0.0/0
`
	c, err := ParseWGConf(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Protocol != "amneziawg-3.0" {
		t.Errorf("AWG3 not detected: Protocol = %q, want amneziawg-3.0", c.Protocol)
	}
	if c.AWG2 == nil {
		t.Fatal("AWG2 settings should not be nil")
	}
	if c.AWG2.HeaderProtectionKey != "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=" {
		t.Errorf("HeaderProtectionKey = %q", c.AWG2.HeaderProtectionKey)
	}
	if c.AWG2.ContentPaddingAddition != "50-100" {
		t.Errorf("ContentPaddingAddition = %q", c.AWG2.ContentPaddingAddition)
	}
	if c.AWG2.RekeyAfterTime != "100-115" {
		t.Errorf("RekeyAfterTime = %q", c.AWG2.RekeyAfterTime)
	}
	if c.AWG2.RekeyTimeout != "4-6" {
		t.Errorf("RekeyTimeout = %q", c.AWG2.RekeyTimeout)
	}
	if c.AWG2.RejectAfterTime != "140-165" {
		t.Errorf("RejectAfterTime = %q", c.AWG2.RejectAfterTime)
	}
	if c.AWG2.KeepaliveTimeout != "8-12" {
		t.Errorf("KeepaliveTimeout = %q", c.AWG2.KeepaliveTimeout)
	}
	if c.AWG2.MaxHandshakeAttempts != "15-25" {
		t.Errorf("MaxHandshakeAttempts = %q", c.AWG2.MaxHandshakeAttempts)
	}
	if c.AWG2.RandomTrailers != "on" {
		t.Errorf("RandomTrailers = %q, want on", c.AWG2.RandomTrailers)
	}
	if c.AWG2.DisableCookies != "on" {
		t.Errorf("DisableCookies = %q, want on", c.AWG2.DisableCookies)
	}
}

// TestParseWGConf_AWG3DetectionViaOffFlagsOnly verifies that a v3 config
// where RandomTrailers/DisableCookies are both "off" (but present, since
// writeAWG3Fields always writes them for a v3 interface) is still detected
// as v3 rather than falling back to v2 — presence, not value, is the marker.
func TestParseWGConf_AWG3DetectionViaOffFlagsOnly(t *testing.T) {
	conf := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.9.0.5/24
Jc = 6
S1 = 32
S3 = 12
S4 = 12
H1 = 100000000-150000000
RandomTrailers = off
DisableCookies = off

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = awg3.example.com:51822
AllowedIPs = 0.0.0.0/0
`
	c, err := ParseWGConf(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Protocol != "amneziawg-3.0" {
		t.Errorf("Protocol = %q, want amneziawg-3.0 (off-only flags should still mark v3)", c.Protocol)
	}
	if c.AWG2.RandomTrailers != "" {
		t.Errorf("RandomTrailers = %q, want empty (off stored as unset, not the literal string)", c.AWG2.RandomTrailers)
	}
}

func TestParseWGConf_CommentsAndBlankLines(t *testing.T) {
	conf := `# This is a comment
; This is also a comment

[Interface]
# inline comment line
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.8.0.5/24

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0
`
	c, err := ParseWGConf(conf)
	if err != nil {
		t.Fatalf("comments/blank lines should be ignored: %v", err)
	}
	if c.PrivateKey == "" {
		t.Error("PrivateKey should be parsed despite surrounding comments")
	}
}

// ── AddressToHost32 ───────────────────────────────────────────────────────────

func TestAddressToHost32(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"10.8.0.5/24", "10.8.0.5/32"},
		{"10.8.0.5/32", "10.8.0.5/32"},
		{"10.8.0.5/16", "10.8.0.5/32"},
		{"192.168.1.100/24", "192.168.1.100/32"},
	}
	for _, tc := range cases {
		got := AddressToHost32(tc.input)
		if got != tc.want {
			t.Errorf("AddressToHost32(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ── subnetsOverlap ────────────────────────────────────────────────────────────

func TestSubnetsOverlap(t *testing.T) {
	cases := []struct {
		cidr1, cidr2 string
		want         bool
	}{
		{"10.8.0.0/24", "10.8.0.5/24", true},  // same subnet
		{"10.8.0.0/24", "10.8.1.0/24", false}, // different /24
		{"10.8.0.0/16", "10.8.5.0/24", true},  // /24 inside /16
		{"10.8.0.1/32", "10.8.0.2/32", false}, // two distinct /32
		{"10.8.0.1/32", "10.8.0.1/32", true},  // same /32
		{"10.0.0.0/8", "192.168.0.0/24", false},
	}
	for _, tc := range cases {
		got := subnetsOverlap(tc.cidr1, tc.cidr2)
		if got != tc.want {
			t.Errorf("subnetsOverlap(%q, %q) = %v, want %v", tc.cidr1, tc.cidr2, got, tc.want)
		}
	}
}

func TestParseWGConf_MTUParsed(t *testing.T) {
	conf := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.8.0.5/24
MTU = 1380

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0
`
	c, err := ParseWGConf(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.MTU != 1380 {
		t.Errorf("MTU = %d, want 1380", c.MTU)
	}
}

func TestParseWGConf_MTUAbsent(t *testing.T) {
	c, err := ParseWGConf(validConf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.MTU != 0 {
		t.Errorf("MTU = %d, want 0 (absent)", c.MTU)
	}
}
