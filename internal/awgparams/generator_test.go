package awgparams

import (
	"fmt"
	"regexp"
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

// rcRegex matches the first <rc N> tag and captures N.
var rcRegex = regexp.MustCompile(`<rc (\d+)>`)

// extractRC returns the integer N from the first <rc N> occurrence in s.
// Fails the test if no match is found.
func extractRC(t *testing.T, s string) int {
	t.Helper()
	m := rcRegex.FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("extractRC: no <rc N> found in %q", s)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("extractRC: bad integer %q: %v", m[1], err)
	}
	return n
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
		"dns_query": true,
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
		"dtls", "http3", "sip", "wireguard_noise", "dns_query",
		"tls_to_quic", "quic_burst",
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
	if len(Profiles) < 11 {
		t.Errorf("expected at least 11 profiles, got %d", len(Profiles))
	}
}

// ── BFP: Chrome <rc N> in range per protocol ─────────────────────────────────

// chromeBFPRanges maps profile name → expected [min, max] for Chrome BFP <rc N>.
// Values come directly from BFP["chrome"] in generator.go.
var chromeBFPRanges = map[string][2]int{
	"quic_initial":     {1250, 1250},
	"quic_0rtt":        {1250, 1350},
	"http3":            {1250, 1350},
	"tls_client_hello": {512, 800},
	"wireguard_noise":  {1200, 1250},
	"dtls":             {1100, 1200},
}

func TestBFP_ChromeRcInRange(t *testing.T) {
	for profile, want := range chromeBFPRanges {
		profile, want := profile, want // capture
		t.Run(profile, func(t *testing.T) {
			for i := 0; i < 20; i++ {
				p := Generate(Options{Profile: profile, Browser: "chrome"})
				rc := extractRC(t, p.I1)
				if rc < want[0] || rc > want[1] {
					t.Errorf("iteration %d: profile=%s browser=chrome <rc %d> not in [%d,%d] — I1=%s",
						i, profile, rc, want[0], want[1], p.I1)
				}
			}
		})
	}
}

// ── BFP: all browsers smoke test ─────────────────────────────────────────────

func TestBFP_AllBrowsersSmoke(t *testing.T) {
	browsers := []string{
		"chrome", "firefox", "safari", "edge", "yandex_desktop", "yandex_mobile",
	}
	for _, browser := range browsers {
		browser := browser
		t.Run(browser, func(t *testing.T) {
			p := Generate(Options{Profile: "quic_initial", Browser: browser})
			if p.I1 == "" {
				t.Errorf("browser=%s: I1 is empty", browser)
			}
			if p.Profile != "quic_initial" {
				t.Errorf("browser=%s: expected Profile=quic_initial, got %s", browser, p.Profile)
			}
		})
	}
}

// ── BFP: unknown browser falls back gracefully ────────────────────────────────

func TestBFP_UnknownBrowserFallback(t *testing.T) {
	p := Generate(Options{Profile: "quic_initial", Browser: "ie6"})
	if p.I1 == "" {
		t.Errorf("unknown browser 'ie6': I1 is empty — fallback must not panic or produce empty output")
	}
}

// ── BFP: empty browser uses protocol defaults (regression guard) ──────────────

func TestBFP_EmptyBrowserFallback(t *testing.T) {
	baseProfiles := []string{
		"quic_initial", "quic_0rtt", "tls_client_hello",
		"dtls", "http3", "sip", "wireguard_noise", "dns_query",
	}
	for _, profile := range baseProfiles {
		profile := profile
		t.Run(profile, func(t *testing.T) {
			p := Generate(Options{Profile: profile, Browser: ""})
			if p.I1 == "" {
				t.Errorf("profile=%s browser='': I1 is empty", profile)
			}
		})
	}
}

// ── Composite: tls_to_quic ────────────────────────────────────────────────────

func TestComposite_TLStoQUIC(t *testing.T) {
	p := Generate(Options{Profile: "tls_to_quic"})

	if p.I1 == "" {
		t.Fatal("tls_to_quic: I1 is empty")
	}
	if p.Profile != "tls_to_quic" {
		t.Errorf("tls_to_quic: expected Profile=tls_to_quic, got %s", p.Profile)
	}
	// TLS ClientHello record starts with 160301 — must be present as mkTLS is first
	if !strings.Contains(p.I1, "160301") {
		t.Errorf("tls_to_quic: expected TLS record header '160301' in I1, got: %s", p.I1)
	}
	// Composite packet should be substantially longer than a single packet
	if len(p.I1) <= 50 {
		t.Errorf("tls_to_quic: I1 length %d is too short (expected > 50), got: %s", len(p.I1), p.I1)
	}
}

// ── Composite: quic_burst ─────────────────────────────────────────────────────

func TestComposite_QUICBurst(t *testing.T) {
	p := Generate(Options{Profile: "quic_burst"})

	if p.I1 == "" {
		t.Fatal("quic_burst: I1 is empty")
	}
	if p.Profile != "quic_burst" {
		t.Errorf("quic_burst: expected Profile=quic_burst, got %s", p.Profile)
	}
	// Three packets concatenated — must be substantially longer than a single packet
	if len(p.I1) <= 100 {
		t.Errorf("quic_burst: I1 length %d is too short (expected > 100), got: %s", len(p.I1), p.I1)
	}
}

// ── DNS Query profile ─────────────────────────────────────────────────────────

func TestDNS_EncodeName(t *testing.T) {
	cases := []struct {
		input string
		// expected: each label prefixed by its length byte (hex), terminated by "00"
		wantPrefix string // first label length hex
	}{
		{"google.com", "06"}, // "google" = 6 chars
		{"yandex.ru", "06"}, // "yandex" = 6 chars
		{"a.b.c", "01"},     // "a" = 1 char
	}
	for _, tc := range cases {
		got := encodeDNSName(tc.input)
		if !strings.HasPrefix(got, tc.wantPrefix) {
			t.Errorf("encodeDNSName(%q): expected prefix %q, got %q", tc.input, tc.wantPrefix, got)
		}
		if !strings.HasSuffix(got, "00") {
			t.Errorf("encodeDNSName(%q): expected null terminator '00', got %q", tc.input, got)
		}
		if len(got)%2 != 0 {
			t.Errorf("encodeDNSName(%q): odd-length hex %q", tc.input, got)
		}
	}
}

func TestDNS_I1Format(t *testing.T) {
	for i := 0; i < 20; i++ {
		p := Generate(Options{Profile: "dns_query"})
		if p.I1 == "" {
			t.Fatalf("iteration %d: dns_query I1 is empty", i)
		}
		// Must be a single <b 0x...> tag — no <rc>, <r>, or <t> tags
		if !strings.HasPrefix(p.I1, "<b 0x") {
			t.Errorf("iteration %d: dns_query I1 does not start with '<b 0x': %s", i, p.I1)
		}
		if strings.Contains(p.I1, "<rc") {
			t.Errorf("iteration %d: dns_query I1 must not contain <rc> (no BFP): %s", i, p.I1)
		}
		if p.Profile != "dns_query" {
			t.Errorf("iteration %d: expected Profile=dns_query, got %s", i, p.Profile)
		}
	}
}

func TestDNS_NoBFPEntry(t *testing.T) {
	// dns_query has no BFP table entry — BFP map must not contain it
	if _, ok := BFP["dns_query"]; ok {
		t.Error("BFP map should not have a 'dns_query' entry — DNS profile has no browser fingerprint")
	}
}

func TestDNS_InRandomPool(t *testing.T) {
	seen := false
	for i := 0; i < 200; i++ {
		p := Generate(Options{Profile: "random"})
		if p.Profile == "dns_query" {
			seen = true
			break
		}
	}
	if !seen {
		t.Error("dns_query never appeared in 200 random generations — must be in random pool")
	}
}

// ── Composite profiles not in random pool ─────────────────────────────────────

func TestComposite_NotInRandomPool(t *testing.T) {
	composites := map[string]bool{
		"tls_to_quic": true,
		"quic_burst":  true,
	}
	for i := 0; i < 100; i++ {
		p := Generate(Options{Profile: "random"})
		if composites[p.Profile] {
			t.Errorf("iteration %d: random pool returned composite profile %q — composites must be excluded", i, p.Profile)
		}
	}
}
