package firewall

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/gateway"
	"github.com/JohnnyVBut/cascade/internal/ipset"
	aliasespkg "github.com/JohnnyVBut/cascade/internal/aliases"
)

func initTestDB(t *testing.T) (*Manager, *aliasespkg.Manager) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cascade-firewall-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	im, err := ipset.New(dir)
	if err != nil {
		t.Fatalf("ipset.New: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
	am := aliasespkg.New(im)
	gm := gateway.NewManager()
	mgr := New(am, gm)
	return mgr, am
}

// ── ipInCIDR ──────────────────────────────────────────────────────────────────

func TestIpInCIDR_MatchesNetworkAddress(t *testing.T) {
	if !ipInCIDR("10.0.0.0", "10.0.0.0/8") {
		t.Error("network address should match its own CIDR")
	}
}

func TestIpInCIDR_MatchesHostInRange(t *testing.T) {
	if !ipInCIDR("10.8.0.5", "10.8.0.0/24") {
		t.Error("10.8.0.5 should be in 10.8.0.0/24")
	}
}

func TestIpInCIDR_NonNetworkAddressInRange(t *testing.T) {
	// This is the FIX-GO-10 regression: host bits set in IP but within CIDR.
	if !ipInCIDR("192.168.100.3", "192.168.100.0/24") {
		t.Error("192.168.100.3 should be in 192.168.100.0/24 (FIX-GO-10)")
	}
}

func TestIpInCIDR_OutsideRange(t *testing.T) {
	if ipInCIDR("10.9.0.1", "10.8.0.0/24") {
		t.Error("10.9.0.1 should NOT be in 10.8.0.0/24")
	}
}

func TestIpInCIDR_HostAddressExactMatch(t *testing.T) {
	// Without slash: exact match comparison
	if !ipInCIDR("8.8.8.8", "8.8.8.8") {
		t.Error("exact host match should return true")
	}
	if ipInCIDR("8.8.8.9", "8.8.8.8") {
		t.Error("different host should not match")
	}
}

func TestIpInCIDR_EmptyInputs(t *testing.T) {
	if ipInCIDR("", "10.0.0.0/8") {
		t.Error("empty IP should return false")
	}
	if ipInCIDR("10.0.0.1", "") {
		t.Error("empty CIDR should return false")
	}
}

func TestIpInCIDR_InvalidInputs(t *testing.T) {
	if ipInCIDR("not-an-ip", "10.0.0.0/8") {
		t.Error("invalid IP should return false")
	}
	if ipInCIDR("10.0.0.1", "bad/cidr") {
		t.Error("invalid CIDR should return false")
	}
}

func TestIpInCIDR_IPv6(t *testing.T) {
	if !ipInCIDR("2001:db8::1", "2001:db8::/32") {
		t.Error("IPv6 address should match IPv6 CIDR")
	}
}

// ── Rule JSON marshaling ──────────────────────────────────────────────────────

func TestRule_JSONRoundTrip(t *testing.T) {
	fwmark := 100
	rule := Rule{
		ID:        "rule-1",
		Name:      "Block KZ",
		Enabled:   true,
		Order:     1,
		Interface: "any",
		Protocol:  "any",
		Source:    Endpoint{Type: "cidr", Value: "10.0.0.0/8"},
		Destination: Endpoint{Type: "any"},
		Action:    "accept",
		GatewayID: "gw-1",
		Fwmark:    &fwmark,
		CreatedAt: "2026-01-01T00:00:00Z",
	}

	data, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got Rule
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.ID != rule.ID {
		t.Errorf("ID = %q, want %q", got.ID, rule.ID)
	}
	if got.Fwmark == nil || *got.Fwmark != 100 {
		t.Errorf("Fwmark = %v, want 100", got.Fwmark)
	}
	if got.Source.Type != "cidr" {
		t.Errorf("Source.Type = %q, want 'cidr'", got.Source.Type)
	}
}

// ── SimulateTrace (DB only, no iptables) ─────────────────────────────────────

func TestSimulateTrace_NoRules_NoMatch(t *testing.T) {
	m, _ := initTestDB(t)

	result, err := m.SimulateTrace("10.8.0.5", "8.8.8.8")
	if err != nil {
		t.Fatalf("SimulateTrace: %v", err)
	}
	if result.MatchedRule != nil {
		t.Errorf("expected no match with empty rule set, got %+v", result.MatchedRule)
	}
	if result.Steps == nil {
		t.Error("Steps should be non-nil (empty slice)")
	}
}

func TestSimulateTrace_CIDRRule_Matches(t *testing.T) {
	m, _ := initTestDB(t)

	fwmark := 42
	rule := Rule{
		ID:          "r1",
		Name:        "Route KZ",
		Enabled:     true,
		Order:       1,
		Interface:   "any",
		Protocol:    "any",
		Source:      Endpoint{Type: "any"},
		Destination: Endpoint{Type: "cidr", Value: "10.0.0.0/8"},
		Action:      "accept",
		GatewayID:   "gw-kz",
		Fwmark:      &fwmark,
		CreatedAt:   "2026-01-01T00:00:00Z",
	}
	if err := insertRule(rule); err != nil {
		t.Fatalf("insertRule: %v", err)
	}

	result, err := m.SimulateTrace("192.168.1.5", "10.5.0.1")
	if err != nil {
		t.Fatalf("SimulateTrace: %v", err)
	}
	if result.MatchedRule == nil {
		t.Fatal("expected a matched rule")
	}
	if result.MatchedRule.ID != "r1" {
		t.Errorf("MatchedRule.ID = %q, want 'r1'", result.MatchedRule.ID)
	}
	if result.MatchedRule.Fwmark == nil || *result.MatchedRule.Fwmark != 42 {
		t.Errorf("MatchedRule.Fwmark = %v, want 42", result.MatchedRule.Fwmark)
	}
}

func TestSimulateTrace_DisabledRule_Skipped(t *testing.T) {
	m, _ := initTestDB(t)

	rule := Rule{
		ID:          "r2",
		Name:        "Disabled",
		Enabled:     false, // disabled — should be skipped
		Order:       1,
		Interface:   "any",
		Protocol:    "any",
		Source:      Endpoint{Type: "any"},
		Destination: Endpoint{Type: "any"},
		Action:      "drop",
		CreatedAt:   "2026-01-01T00:00:00Z",
	}
	if err := insertRule(rule); err != nil {
		t.Fatalf("insertRule: %v", err)
	}

	result, err := m.SimulateTrace("1.2.3.4", "5.6.7.8")
	if err != nil {
		t.Fatalf("SimulateTrace: %v", err)
	}
	if result.MatchedRule != nil {
		t.Errorf("disabled rule should not match, got %+v", result.MatchedRule)
	}
}

func TestSimulateTrace_FirstMatchWins(t *testing.T) {
	m, _ := initTestDB(t)

	fwmark1 := 10
	fwmark2 := 20
	rule1 := Rule{
		ID: "first", Name: "First", Enabled: true, Order: 1,
		Interface: "any", Protocol: "any",
		Source: Endpoint{Type: "any"}, Destination: Endpoint{Type: "any"},
		Action: "accept", GatewayID: "gw1", Fwmark: &fwmark1,
		CreatedAt: "2026-01-01T00:00:00Z",
	}
	rule2 := Rule{
		ID: "second", Name: "Second", Enabled: true, Order: 2,
		Interface: "any", Protocol: "any",
		Source: Endpoint{Type: "any"}, Destination: Endpoint{Type: "any"},
		Action: "drop", GatewayID: "gw2", Fwmark: &fwmark2,
		CreatedAt: "2026-01-01T00:00:01Z",
	}
	insertRule(rule1)
	insertRule(rule2)

	result, err := m.SimulateTrace("1.2.3.4", "5.6.7.8")
	if err != nil {
		t.Fatalf("SimulateTrace: %v", err)
	}
	if result.MatchedRule == nil {
		t.Fatal("expected a match")
	}
	if result.MatchedRule.ID != "first" {
		t.Errorf("expected first rule to win, got %q", result.MatchedRule.ID)
	}
}

func TestSimulateTrace_InvertedCIDRSource(t *testing.T) {
	m, _ := initTestDB(t)

	// Match source NOT in 10.0.0.0/8
	rule := Rule{
		ID: "inv", Name: "Inverted", Enabled: true, Order: 1,
		Interface: "any", Protocol: "any",
		Source:      Endpoint{Type: "cidr", Value: "10.0.0.0/8", Invert: true},
		Destination: Endpoint{Type: "any"},
		Action: "drop",
		CreatedAt: "2026-01-01T00:00:00Z",
	}
	insertRule(rule)

	// Source 192.168.1.1 is NOT in 10.0.0.0/8 → inverted match = true.
	result, _ := m.SimulateTrace("192.168.1.1", "8.8.8.8")
	if result.MatchedRule == nil {
		t.Error("expected match for IP outside inverted CIDR")
	}

	// Source 10.8.0.5 IS in 10.0.0.0/8 → inverted match = false.
	result2, _ := m.SimulateTrace("10.8.0.5", "8.8.8.8")
	if result2.MatchedRule != nil {
		t.Error("expected no match for IP inside inverted CIDR")
	}
}

// ── GetRules CRUD ─────────────────────────────────────────────────────────────

func TestGetRules_EmptyOnFreshDB(t *testing.T) {
	m, _ := initTestDB(t)
	rules, err := m.GetRules()
	if err != nil {
		t.Fatalf("GetRules: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
	}
}

func TestGetRule_NotFound(t *testing.T) {
	m, _ := initTestDB(t)
	rule, err := m.GetRule("no-such-id")
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if rule != nil {
		t.Errorf("expected nil for unknown ID, got %+v", rule)
	}
}
