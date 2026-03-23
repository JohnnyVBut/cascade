package validate

import (
	"strings"
	"testing"
)

// ---- IfaceName ----

func TestIfaceName_Valid(t *testing.T) {
	cases := []string{
		"eth0",
		"wg10",
		"bond-1",
		"ens3.100",
		"lo",
		"eth0_vlan",
		"a",
		"abcdefghijklmno", // exactly 15 chars
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if err := IfaceName(name); err != nil {
				t.Errorf("IfaceName(%q) returned unexpected error: %v", name, err)
			}
		})
	}
}

func TestIfaceName_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"shell injection semicolon", "eth0; id"},
		{"space in name", "a b"},
		{"longer than 15 chars", "abcdefghijklmnop"},
		{"path traversal", "../../etc"},
		{"dollar sign", "eth$0"},
		{"at sign", "eth@0"},
		{"slash", "eth/0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := IfaceName(tc.input); err == nil {
				t.Errorf("IfaceName(%q) expected error, got nil", tc.input)
			}
		})
	}
}

// ---- TableName ----

func TestTableName_Valid(t *testing.T) {
	cases := []string{
		"main",
		"default",
		"100",
		"vpn_kz",
		"vpn-kz",
		"local",
		"a",
		"abcdefghijklmnopqrstuvwxyz01234", // exactly 31 chars
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if err := TableName(name); err != nil {
				t.Errorf("TableName(%q) returned unexpected error: %v", name, err)
			}
		})
	}
}

func TestTableName_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"shell injection semicolon", "main; id"},
		{"space in name", "table name"},
		{"longer than 31 chars", "abcdefghijklmnopqrstuvwxyz012345"},
		{"dot in name", "table.name"},
		{"slash", "vpn/kz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := TableName(tc.input); err == nil {
				t.Errorf("TableName(%q) expected error, got nil", tc.input)
			}
		})
	}
}

// ---- IP ----

func TestIP_Valid(t *testing.T) {
	cases := []string{
		"8.8.8.8",
		"10.0.0.1",
		"192.168.1.254",
		"0.0.0.0",
		"255.255.255.255",
		"2001:db8::1",
		"::1",
		"fe80::1",
		"::",
	}
	for _, ip := range cases {
		t.Run(ip, func(t *testing.T) {
			if err := IP(ip); err != nil {
				t.Errorf("IP(%q) returned unexpected error: %v", ip, err)
			}
		})
	}
}

func TestIP_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"plain hostname", "not-an-ip"},
		{"shell injection", "8.8.8.8; id"},
		{"octet out of range", "300.1.2.3"},
		{"cidr notation", "10.0.0.1/24"},
		{"partial address", "10.0.0"},
		{"extra octet", "10.0.0.1.2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := IP(tc.input); err == nil {
				t.Errorf("IP(%q) expected error, got nil", tc.input)
			}
		})
	}
}

// ---- IpsetName ----

func TestIpsetName_Valid(t *testing.T) {
	cases := []string{
		"myset",
		"vpn_ru",
		"cascade_ipset",
		"a",
		strings.Repeat("a", 31), // exactly 31 chars — kernel limit
	}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			if err := IpsetName(name); err != nil {
				t.Errorf("IpsetName(%q) returned unexpected error: %v", name, err)
			}
		})
	}
}

func TestIpsetName_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"shell injection semicolon", "myset; id"},
		{"hyphen not allowed", "my-set"},
		{"space in name", "my set"},
		{"longer than 31 chars", strings.Repeat("a", 32)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := IpsetName(tc.input); err == nil {
				t.Errorf("IpsetName(%q) expected error, got nil", tc.input)
			}
		})
	}
}

// ---- HostOrIP ----

func TestHostOrIP_Valid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty string (not set)", ""},
		{"IPv4 address", "8.8.8.8"},
		{"IPv6 loopback", "::1"},
		{"simple hostname", "google.com"},
		{"multi-label hostname with hyphen", "my-host.example.org"},
		{"mixed-case hostname", "Example.COM"},
		{"three single-char labels", "a.b.c"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := HostOrIP(tc.input); err != nil {
				t.Errorf("HostOrIP(%q) returned unexpected error: %v", tc.input, err)
			}
		})
	}
}

func TestHostOrIP_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"shell injection semicolon", "google.com; id"},
		{"space in name", "host name"},
		{"command substitution", "$(id)"},
		{"label exceeds 63 chars", strings.Repeat("a", 64) + ".com"},
		{"total exceeds 253 chars", strings.Repeat("a", 50) + "." + strings.Repeat("b", 50) + "." + strings.Repeat("c", 50) + "." + strings.Repeat("d", 50) + "." + strings.Repeat("e", 55)},
		{"double dot (empty label)", "host..double.dot"},
		{"leading dash in label", "-leading.dash"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := HostOrIP(tc.input); err == nil {
				t.Errorf("HostOrIP(%q) expected error, got nil", tc.input)
			}
		})
	}
}

// ---- WGKey ----

// TestWGKey_Valid verifies that well-formed 32-byte base64-encoded keys are accepted.
func TestWGKey_Valid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		// 32 zero bytes in standard base64 — 44 chars with trailing =
		{"32 zero bytes", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
		// All-ones (0xFF) 32-byte value
		{"32 0xFF bytes", "//////////////////////////////////////////8="},
		// Sequential bytes 0x01..0x20 — deterministic non-trivial value
		{"bytes 1 to 32", "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA="},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := WGKey(tc.input); err != nil {
				t.Errorf("WGKey(%q) returned unexpected error: %v", tc.input, err)
			}
		})
	}
}

// TestWGKey_Invalid verifies that malformed keys — including newline injection,
// the primary attack vector against wg/awg config files — are rejected.
func TestWGKey_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		// PRIMARY ATTACK VECTOR: newline injection into wg/awg config file.
		// An attacker-controlled PrivateKey field containing "\nPostUp = id"
		// would terminate the [Interface] section early and inject an arbitrary
		// iptables/shell command that runs as root when the interface starts.
		{"newline injection PostUp", "fake\nPostUp = id"},
		// Empty string — AddPeer guard checks this before WGKey, but WGKey itself
		// must also reject it for use as a standalone validator.
		{"empty string", ""},
		// Garbage that is neither base64 nor a key.
		{"not base64", "notbase64!!!"},
		// Valid base64 but only 16 bytes — WireGuard keys must be exactly 32 bytes.
		{"base64 but 16 bytes", "AAAAAAAAAAAAAAAAAAAAAA=="},
		// Valid base64, 44 chars without padding — decodes to 33 bytes (one too many).
		{"base64 but 33 bytes", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		// Shell metacharacter injection via backtick substitution.
		{"backtick command substitution", "`id`"},
		// Semicolon — common shell injection delimiter.
		{"semicolon injection", "AAAAA;BBBBB"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := WGKey(tc.input); err == nil {
				t.Errorf("WGKey(%q) expected error, got nil", tc.input)
			}
		})
	}
}
