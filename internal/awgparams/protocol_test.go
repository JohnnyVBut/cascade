package awgparams

import (
	"os"
	"testing"
)

// TestIsAmneziaWG covers item 2 of the AWG3-protocol-redo test plan:
// IsAmneziaWG must be true for both AmneziaWG protocol lines and false for
// everything else, including WireGuard 1.0 and the empty string.
func TestIsAmneziaWG(t *testing.T) {
	cases := []struct {
		protocol string
		want     bool
	}{
		{"amneziawg-2.0", true},
		{"amneziawg-3.0", true},
		{"wireguard-1.0", false},
		{"", false},
		{"amneziawg-1.0", false},
		{"AMNEZIAWG-2.0", false}, // exact-match only, no case folding
		{"amneziawg-2.0 ", false},
		{"random-garbage", false},
	}
	for _, c := range cases {
		t.Run(c.protocol, func(t *testing.T) {
			got := IsAmneziaWG(c.protocol)
			if got != c.want {
				t.Errorf("IsAmneziaWG(%q) = %v, want %v", c.protocol, got, c.want)
			}
		})
	}
}

// ── IsUserspaceMode ───────────────────────────────────────────────────────────
// Moved here from internal/tunnel so tunnel and api (both callers) share one
// tested implementation instead of duplicating the os.Getenv check.

// TestIsUserspaceMode_WhenEnvSet verifies that IsUserspaceMode returns true
// when WG_QUICK_USERSPACE_IMPLEMENTATION is set to "amneziawg-go".
func TestIsUserspaceMode_WhenEnvSet(t *testing.T) {
	if err := os.Setenv("WG_QUICK_USERSPACE_IMPLEMENTATION", "amneziawg-go"); err != nil {
		t.Fatalf("os.Setenv: %v", err)
	}
	defer os.Unsetenv("WG_QUICK_USERSPACE_IMPLEMENTATION")

	if !IsUserspaceMode() {
		t.Error("IsUserspaceMode() = false, want true when WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go")
	}
}

// TestIsUserspaceMode_WhenEnvEmpty verifies that IsUserspaceMode returns false
// when WG_QUICK_USERSPACE_IMPLEMENTATION is unset or empty (kernel mode).
func TestIsUserspaceMode_WhenEnvEmpty(t *testing.T) {
	os.Unsetenv("WG_QUICK_USERSPACE_IMPLEMENTATION")

	if IsUserspaceMode() {
		t.Error("IsUserspaceMode() = true, want false when WG_QUICK_USERSPACE_IMPLEMENTATION is unset")
	}
}

// TestIsUserspaceMode_WhenEnvOtherValue verifies that IsUserspaceMode returns
// false when WG_QUICK_USERSPACE_IMPLEMENTATION is set to a value other than
// "amneziawg-go" (e.g. "wireguard-go" — the upstream WireGuard userspace impl).
func TestIsUserspaceMode_WhenEnvOtherValue(t *testing.T) {
	if err := os.Setenv("WG_QUICK_USERSPACE_IMPLEMENTATION", "wireguard-go"); err != nil {
		t.Fatalf("os.Setenv: %v", err)
	}
	defer os.Unsetenv("WG_QUICK_USERSPACE_IMPLEMENTATION")

	if IsUserspaceMode() {
		t.Error("IsUserspaceMode() = true, want false when WG_QUICK_USERSPACE_IMPLEMENTATION=wireguard-go")
	}
}
