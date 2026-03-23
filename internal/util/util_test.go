package util

import (
	"errors"
	"testing"
)

// ── IsValidIPv4 ───────────────────────────────────────────────────────────────

func TestIsValidIPv4_Valid(t *testing.T) {
	cases := []string{
		"0.0.0.0",
		"10.0.0.1",
		"192.168.1.254",
		"255.255.255.255",
		"172.16.0.1",
	}
	for _, ip := range cases {
		t.Run(ip, func(t *testing.T) {
			if !IsValidIPv4(ip) {
				t.Errorf("IsValidIPv4(%q) = false, want true", ip)
			}
		})
	}
}

func TestIsValidIPv4_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"ipv6", "::1"},
		{"hostname", "example.com"},
		{"three octets", "10.0.0"},
		{"five octets", "10.0.0.1.2"},
		{"octet 256", "10.0.0.256"},
		{"negative octet", "10.0.0.-1"},
		{"letters in octet", "10.0.a.1"},
		{"cidr notation", "10.0.0.0/24"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsValidIPv4(tc.input) {
				t.Errorf("IsValidIPv4(%q) = true, want false", tc.input)
			}
		})
	}
}

// ── IsValidIP ─────────────────────────────────────────────────────────────────

func TestIsValidIP_Valid(t *testing.T) {
	cases := []string{
		"8.8.8.8",
		"10.0.0.1",
		"::1",
		"2001:db8::1",
		"fe80::1",
		"::",
	}
	for _, ip := range cases {
		t.Run(ip, func(t *testing.T) {
			if !IsValidIP(ip) {
				t.Errorf("IsValidIP(%q) = false, want true", ip)
			}
		})
	}
}

func TestIsValidIP_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"hostname", "example.com"},
		{"partial", "10.0.0"},
		{"cidr", "10.0.0.0/24"},
		{"shell injection", "8.8.8.8; id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsValidIP(tc.input) {
				t.Errorf("IsValidIP(%q) = true, want false", tc.input)
			}
		})
	}
}

// ── IsValidCIDR ───────────────────────────────────────────────────────────────

func TestIsValidCIDR_Valid(t *testing.T) {
	cases := []string{
		"10.0.0.0/8",
		"192.168.1.0/24",
		"0.0.0.0/0",
		"10.8.0.2/32",
		"2001:db8::/32",
		"::/0",
	}
	for _, cidr := range cases {
		t.Run(cidr, func(t *testing.T) {
			if !IsValidCIDR(cidr) {
				t.Errorf("IsValidCIDR(%q) = false, want true", cidr)
			}
		})
	}
}

func TestIsValidCIDR_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"plain IP", "10.0.0.1"},
		{"hostname", "example.com/24"},
		{"prefix too large", "10.0.0.0/33"},
		{"shell injection", "10.0.0.0/8; id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsValidCIDR(tc.input) {
				t.Errorf("IsValidCIDR(%q) = true, want false", tc.input)
			}
		})
	}
}

// ── ExecError.Error() ─────────────────────────────────────────────────────────

func TestExecError_WithStderr(t *testing.T) {
	e := &ExecError{
		Err:    errors.New("exit status 1"),
		Stderr: "RTNETLINK answers: File exists\n",
		Cmd:    "ip route add ...",
	}
	got := e.Error()
	want := "exit status 1: RTNETLINK answers: File exists"
	if got != want {
		t.Errorf("ExecError.Error() = %q, want %q", got, want)
	}
}

func TestExecError_WithEmptyStderr(t *testing.T) {
	e := &ExecError{
		Err:    errors.New("exit status 2"),
		Stderr: "",
		Cmd:    "wg show",
	}
	got := e.Error()
	want := "exit status 2"
	if got != want {
		t.Errorf("ExecError.Error() = %q, want %q", got, want)
	}
}

func TestExecError_WithWhitespaceOnlyStderr(t *testing.T) {
	e := &ExecError{
		Err:    errors.New("exit status 1"),
		Stderr: "   \n\t  ",
		Cmd:    "some cmd",
	}
	// TrimSpace on whitespace-only string => "", so it falls through to Err.Error()
	got := e.Error()
	want := "exit status 1"
	if got != want {
		t.Errorf("ExecError.Error() = %q, want %q", got, want)
	}
}
