package awgparams

import "testing"

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
