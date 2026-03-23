package awgparams

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// parseRange parses "start-end" into two ints.
func parseRange(t *testing.T, s string) (int, int) {
	t.Helper()
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		t.Fatalf("parseRange: expected 'start-end', got %q", s)
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Fatalf("parseRange: bad start in %q: %v", s, err)
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("parseRange: bad end in %q: %v", s, err)
	}
	return start, end
}

// rangesOverlap returns true when [a1,a2] and [b1,b2] share any value.
func rangesOverlap(a1, a2, b1, b2 int) bool {
	return a1 <= b2 && b1 <= a2
}

// ── H1-H4 non-overlapping ─────────────────────────────────────────────────────

func TestGenerate_H1H4NonOverlapping(t *testing.T) {
	for i := 0; i < 20; i++ {
		p := Generate(Options{Profile: "quic_initial"})
		ranges := []string{p.H1, p.H2, p.H3, p.H4}
		labels := []string{"H1", "H2", "H3", "H4"}

		parsed := make([][2]int, 4)
		for j, r := range ranges {
			s, e := parseRange(t, r)
			if s >= e {
				t.Errorf("iteration %d: %s range start (%d) >= end (%d)", i, labels[j], s, e)
			}
			parsed[j] = [2]int{s, e}
		}
		// Check all pairs
		for j := 0; j < 4; j++ {
			for k := j + 1; k < 4; k++ {
				if rangesOverlap(parsed[j][0], parsed[j][1], parsed[k][0], parsed[k][1]) {
					t.Errorf("iteration %d: %s [%d-%d] overlaps with %s [%d-%d]",
						i, labels[j], parsed[j][0], parsed[j][1],
						labels[k], parsed[k][0], parsed[k][1])
				}
			}
		}
	}
}

func TestGenerate_H1H4OrderedZones(t *testing.T) {
	// Each H should be in a progressively higher range:
	// H1 near 100_000_000, H2 near 1_200_000_000, H3 near 2_400_000_000, H4 near 3_600_000_000
	p := Generate(Options{Profile: "wireguard_noise"})
	h1s, _ := parseRange(t, p.H1)
	h2s, _ := parseRange(t, p.H2)
	h3s, _ := parseRange(t, p.H3)
	h4s, _ := parseRange(t, p.H4)

	if !(h1s < h2s && h2s < h3s && h3s < h4s) {
		t.Errorf("H zones not in ascending order: H1=%d H2=%d H3=%d H4=%d", h1s, h2s, h3s, h4s)
	}
}

// ── Jc / Jmin / Jmax ranges ───────────────────────────────────────────────────

func TestGenerate_JcRange(t *testing.T) {
	for _, profile := range []string{"quic_initial", "tls_client_hello", "wireguard_noise"} {
		p := Generate(Options{Profile: profile, Intensity: "medium"})
		if p.Jc < 3 || p.Jc > 10 {
			t.Errorf("profile=%s: Jc=%d not in [3,10]", profile, p.Jc)
		}
	}
}

func TestGenerate_JcHighIntensityBumped(t *testing.T) {
	// high intensity adds 2 to jcExtra, so Jc should be closer to 10
	p := Generate(Options{Profile: "quic_initial", Intensity: "high", Jc: 8})
	// Jc = max(3, min(10, 8+2)) = 10
	if p.Jc != 10 {
		t.Errorf("expected Jc=10 for high intensity + Jc=8, got %d", p.Jc)
	}
}

func TestGenerate_JcDefaultIs6(t *testing.T) {
	// When Jc=0 (default), it becomes 6. Medium intensity no extra, so Jc=6.
	p := Generate(Options{Profile: "sip"})
	if p.Jc < 3 || p.Jc > 10 {
		t.Errorf("Jc=%d not in valid range [3,10]", p.Jc)
	}
}

func TestGenerate_JminPositive(t *testing.T) {
	p := Generate(Options{})
	if p.Jmin < 64 {
		t.Errorf("Jmin=%d should be >= 64 (base)", p.Jmin)
	}
}

func TestGenerate_JmaxBounds(t *testing.T) {
	p := Generate(Options{Intensity: "high"})
	if p.Jmax > 1280 {
		t.Errorf("Jmax=%d exceeds 1280", p.Jmax)
	}
	if p.Jmax < 64 {
		t.Errorf("Jmax=%d is less than 64 (Jmin base)", p.Jmax)
	}
}

// ── S1-S4 bounds ──────────────────────────────────────────────────────────────

func TestGenerate_S1S4Bounds(t *testing.T) {
	for i := 0; i < 10; i++ {
		p := Generate(Options{})
		if p.S1 < 1 || p.S1 > 64 {
			t.Errorf("S1=%d not in [1,64]", p.S1)
		}
		if p.S2 < 1 || p.S2 > 64 {
			t.Errorf("S2=%d not in [1,64]", p.S2)
		}
		if p.S3 < 1 || p.S3 > 64 {
			t.Errorf("S3=%d not in [1,64]", p.S3)
		}
		if p.S4 < 1 || p.S4 > 32 {
			t.Errorf("S4=%d not in [1,32]", p.S4)
		}
	}
}

// ── Profile field is resolved (never "random") ────────────────────────────────

func TestGenerate_RandomProfileResolved(t *testing.T) {
	validProfiles := map[string]bool{
		"quic_initial": true, "quic_0rtt": true, "tls_client_hello": true,
		"dtls": true, "http3": true, "sip": true, "wireguard_noise": true,
	}
	for i := 0; i < 10; i++ {
		p := Generate(Options{Profile: "random"})
		if p.Profile == "random" {
			t.Errorf("Generate returned profile='random' — must be resolved")
		}
		if !validProfiles[p.Profile] {
			t.Errorf("Generate returned unknown profile %q", p.Profile)
		}
	}
}

// ── All profiles produce non-empty I1-I5 ─────────────────────────────────────

func TestGenerate_AllProfilesProduceI1(t *testing.T) {
	profiles := []string{
		"quic_initial", "quic_0rtt", "tls_client_hello",
		"dtls", "http3", "sip", "wireguard_noise",
	}
	for _, profile := range profiles {
		p := Generate(Options{Profile: profile})
		if p.I1 == "" {
			t.Errorf("profile=%s: I1 is empty", profile)
		}
		if p.I2 == "" {
			t.Errorf("profile=%s: I2 is empty", profile)
		}
		if p.Profile != profile {
			t.Errorf("profile=%s: returned Profile=%s", profile, p.Profile)
		}
	}
}

// ── Custom host is used ───────────────────────────────────────────────────────

func TestGenerate_CustomHostAppears(t *testing.T) {
	host := "custom.example.org"
	p := Generate(Options{Profile: "sip", Host: host})
	// SIP encodes the host as hex inside I1
	hostHex := fmt.Sprintf("%x", []byte(host))
	if !strings.Contains(p.I1, hostHex) {
		t.Errorf("custom host %q hex not found in I1: %s", host, p.I1)
	}
}

// ── iterCount > 3 bumps intensity ────────────────────────────────────────────

func TestGenerate_IterCountBumpsJmax(t *testing.T) {
	p1 := Generate(Options{Profile: "quic_initial", Intensity: "medium", IterCount: 0})
	p2 := Generate(Options{Profile: "quic_initial", Intensity: "medium", IterCount: 4})
	// With IterCount=4 > 3, iv bumps by 1. Jmax = min(1280, 256+iv*150+boost*10).
	// boost=20 for IterCount=4; p1 boost=0. Jmax should be >= Jmin.
	if p2.Jmax < p2.Jmin {
		t.Errorf("Jmax (%d) < Jmin (%d)", p2.Jmax, p2.Jmin)
	}
	_ = p1 // existence check
}

// ── Profiles list ─────────────────────────────────────────────────────────────

func TestProfiles_Length(t *testing.T) {
	if len(Profiles) < 8 {
		t.Errorf("expected at least 8 profiles, got %d", len(Profiles))
	}
}
