// ssrf_test.go — tests for the SSRF guard (validator + safe dialer).
package remoteclient

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

// ── isBlockedIP ──────────────────────────────────────────────────────────────

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		// Public — allowed.
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"198.51.100.1", false}, // TEST-NET-2, not in any blocked range
		{"2606:4700:4700::1111", false},
		// Loopback.
		{"127.0.0.1", true},
		{"127.1.2.3", true},
		{"::1", true},
		{"::ffff:127.0.0.1", true}, // IPv4-mapped loopback
		// Private (RFC1918).
		{"10.0.0.1", true},
		{"172.16.5.4", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"fd00::1", true}, // IPv6 ULA
		// Link-local.
		{"169.254.10.20", true},
		{"fe80::1", true},
		// CGNAT.
		{"100.64.0.1", true},
		{"100.127.255.255", true},
		// Unspecified.
		{"0.0.0.0", true},
		{"::", true},
		// Multicast.
		{"224.0.0.1", true},
		{"ff02::1", true},
		// Reserved / broadcast (240.0.0.0/4).
		{"240.0.0.1", true},
		{"255.255.255.255", true},
		// 6to4 (2002::/16) — embeds an IPv4 address, could encode 127.0.0.1 or 192.168.x.x.
		{"2002:7f00:0001::", true}, // embeds 127.0.0.1
		{"2002:c0a8:0101::", true}, // embeds 192.168.1.1
		// NAT64 well-known prefix (64:ff9b::/96) — low 32 bits are a raw IPv4.
		{"64:ff9b::7f00:1", true},   // encodes 127.0.0.1
		{"64:ff9b::c0a8:101", true}, // encodes 192.168.1.1
		// NAT64 local-use prefix (64:ff9b:1::/48, RFC 8215).
		{"64:ff9b:1::7f00:1", true},   // encodes 127.0.0.1
		{"64:ff9b:1::c0a8:101", true}, // encodes 192.168.1.1
	}
	for _, c := range cases {
		addr := netip.MustParseAddr(c.ip)
		if got := isBlockedIP(addr); got != c.blocked {
			t.Errorf("isBlockedIP(%s) = %v, want %v", c.ip, got, c.blocked)
		}
	}
}

// ── ValidateRemoteURL ────────────────────────────────────────────────────────

func TestValidateRemoteURL_PublicLiteral_OK(t *testing.T) {
	for _, raw := range []string{
		"https://8.8.8.8",
		"http://1.1.1.1:8080",
		"https://198.51.100.1/admin",
		"https://[2606:4700:4700::1111]",
	} {
		if err := ValidateRemoteURL(raw); err != nil {
			t.Errorf("ValidateRemoteURL(%q) = %v, want nil", raw, err)
		}
	}
}

func TestValidateRemoteURL_BlockedLiteral_Rejected(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:8080",
		"https://127.1",                // short form parses? if not, NXDOMAIN — still rejected
		"http://[::1]",                 // IPv6 loopback
		"http://[::ffff:127.0.0.1]",    // IPv4-mapped loopback
		"http://10.0.0.5",              // private
		"http://192.168.1.1",           // private
		"http://172.16.0.1",            // private
		"http://169.254.169.254",       // link-local (cloud metadata)
		"http://100.64.0.1",            // CGNAT
		"http://0.0.0.0",               // unspecified
		"http://[fd00::1]",             // IPv6 ULA
	} {
		if err := ValidateRemoteURL(raw); err == nil {
			t.Errorf("ValidateRemoteURL(%q) = nil, want rejection", raw)
		}
	}
}

func TestValidateRemoteURL_Localhost_Rejected(t *testing.T) {
	// "localhost" resolves (locally, no external DNS) to a loopback address.
	if err := ValidateRemoteURL("http://localhost:8080"); err == nil {
		t.Error("ValidateRemoteURL(localhost) = nil, want rejection")
	}
}

func TestValidateRemoteURL_BadScheme_Rejected(t *testing.T) {
	for _, raw := range []string{
		"ftp://example.com",
		"file:///etc/passwd",
		"gopher://8.8.8.8",
		"8.8.8.8", // no scheme
	} {
		if err := ValidateRemoteURL(raw); err == nil {
			t.Errorf("ValidateRemoteURL(%q) = nil, want scheme rejection", raw)
		}
	}
}

func TestValidateRemoteURL_EmptyHost_Rejected(t *testing.T) {
	if err := ValidateRemoteURL("https://"); err == nil {
		t.Error("ValidateRemoteURL(no host) = nil, want rejection")
	}
}

// ── SafeDialContext ──────────────────────────────────────────────────────────

func TestSafeDialContext_BlockedLiteral_Rejected(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1:80",
		"192.168.1.1:443",
		"169.254.169.254:80",
		"[::1]:80",
	} {
		conn, err := SafeDialContext(context.Background(), "tcp", addr)
		if err == nil {
			conn.Close()
			t.Errorf("SafeDialContext(%q) succeeded, want rejection", addr)
		}
	}
}

func TestSafeDialContext_LocalhostName_Rejected(t *testing.T) {
	conn, err := SafeDialContext(context.Background(), "tcp", "localhost:80")
	if err == nil {
		conn.Close()
		t.Fatal("SafeDialContext(localhost) succeeded, want rejection")
	}
	// The error may be "public address" block or a DNS/connect error depending on
	// the environment — either way, the connection must not succeed.
}

// A public address must pass the block check and proceed to the actual dial.
// We cancel the context up front so the dial returns immediately with a
// context error — proving the guard let it through rather than blocking it.
func TestSafeDialContext_PublicLiteral_PassesGuard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before dialing

	_, err := SafeDialContext(ctx, "tcp", "198.51.100.1:80")
	if err == nil {
		t.Fatal("expected an error from cancelled context")
	}
	if strings.Contains(err.Error(), "public address") {
		t.Errorf("public IP was blocked by the guard: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Logf("dial error = %v (expected context.Canceled; acceptable if it reached the dialer)", err)
	}
}
