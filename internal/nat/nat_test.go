package nat

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
)

func initTestDB(t *testing.T) *Manager {
	t.Helper()
	dir, err := os.MkdirTemp("", "cascade-nat-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
	return New(nil) // nil aliases.Manager — alias-based source resolution is skipped
}

// ── isIPOrCIDR ────────────────────────────────────────────────────────────────

func TestIsIPOrCIDR_Valid(t *testing.T) {
	cases := []string{
		"10.0.0.0/24",
		"192.168.1.0/16",
		"1.2.3.4",
		"0.0.0.0/0",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			if !isIPOrCIDR(s) {
				t.Errorf("isIPOrCIDR(%q) = false, want true", s)
			}
		})
	}
}

func TestIsIPOrCIDR_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"letters", "abc"},
		{"shell injection", "10.0.0.0; id"},
		{"ipv6", "::1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isIPOrCIDR(tc.input) {
				t.Errorf("isIPOrCIDR(%q) = true, want false", tc.input)
			}
		})
	}
}

// ── isIPv4Addr ────────────────────────────────────────────────────────────────

func TestIsIPv4Addr_Valid(t *testing.T) {
	cases := []string{"1.2.3.4", "10.0.0.1", "255.255.255.255"}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			if !isIPv4Addr(s) {
				t.Errorf("isIPv4Addr(%q) = false, want true", s)
			}
		})
	}
}

func TestIsIPv4Addr_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"cidr", "10.0.0.0/24"},
		{"letters", "abc"},
		{"shell injection", "1.2.3.4; rm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isIPv4Addr(tc.input) {
				t.Errorf("isIPv4Addr(%q) = true, want false", tc.input)
			}
		})
	}
}

// ── validate ──────────────────────────────────────────────────────────────────

func TestValidate_Valid_MASQUERADE(t *testing.T) {
	m := &Manager{}
	inp := NatRuleInput{
		Name:         "My Rule",
		OutInterface: "eth0",
		Type:         "MASQUERADE",
	}
	if err := m.validate(inp); err != nil {
		t.Errorf("validate valid MASQUERADE: %v", err)
	}
}

func TestValidate_Valid_SNAT(t *testing.T) {
	m := &Manager{}
	inp := NatRuleInput{
		Name:         "SNAT Rule",
		OutInterface: "eth0",
		Type:         "SNAT",
		ToSource:     "1.2.3.4",
	}
	if err := m.validate(inp); err != nil {
		t.Errorf("validate valid SNAT: %v", err)
	}
}

func TestValidate_EmptyName_Error(t *testing.T) {
	m := &Manager{}
	inp := NatRuleInput{OutInterface: "eth0", Type: "MASQUERADE"}
	if err := m.validate(inp); err == nil {
		t.Error("expected error for empty name, got nil")
	}
}

func TestValidate_EmptyInterface_Error(t *testing.T) {
	m := &Manager{}
	inp := NatRuleInput{Name: "test", Type: "MASQUERADE"}
	if err := m.validate(inp); err == nil {
		t.Error("expected error for empty outInterface, got nil")
	}
}

func TestValidate_InvalidInterfaceName_Error(t *testing.T) {
	m := &Manager{}
	inp := NatRuleInput{Name: "test", OutInterface: "eth0; rm -rf /", Type: "MASQUERADE"}
	if err := m.validate(inp); err == nil {
		t.Error("expected error for shell injection in interface, got nil")
	}
}

func TestValidate_InvalidType_Error(t *testing.T) {
	m := &Manager{}
	inp := NatRuleInput{Name: "test", OutInterface: "eth0", Type: "DNAT"}
	if err := m.validate(inp); err == nil {
		t.Error("expected error for invalid type 'DNAT', got nil")
	}
}

func TestValidate_SNATRequiresToSource(t *testing.T) {
	m := &Manager{}
	inp := NatRuleInput{Name: "test", OutInterface: "eth0", Type: "SNAT", ToSource: ""}
	if err := m.validate(inp); err == nil {
		t.Error("expected error for SNAT without toSource, got nil")
	}
}

func TestValidate_SNATBadToSourceIP(t *testing.T) {
	m := &Manager{}
	inp := NatRuleInput{Name: "test", OutInterface: "eth0", Type: "SNAT", ToSource: "not-an-ip"}
	if err := m.validate(inp); err == nil {
		t.Error("expected error for SNAT with non-IP toSource, got nil")
	}
}

func TestValidate_InvalidSourceCIDR(t *testing.T) {
	m := &Manager{}
	inp := NatRuleInput{
		Name: "test", OutInterface: "eth0", Type: "MASQUERADE",
		Source: "bad-cidr",
	}
	if err := m.validate(inp); err == nil {
		t.Error("expected error for invalid source CIDR, got nil")
	}
}

// ── NatRule JSON marshaling ───────────────────────────────────────────────────

func TestNatRule_JSONRoundTrip(t *testing.T) {
	rule := NatRule{
		ID:           "abc-123",
		Name:         "Test Rule",
		Source:       "10.8.0.0/24",
		OutInterface: "eth0",
		Type:         "MASQUERADE",
		Enabled:      true,
		OrderIdx:     1,
		CreatedAt:    "2026-01-01T00:00:00Z",
	}

	data, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got NatRule
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.ID != rule.ID {
		t.Errorf("ID = %q, want %q", got.ID, rule.ID)
	}
	if got.Type != "MASQUERADE" {
		t.Errorf("Type = %q, want 'MASQUERADE'", got.Type)
	}
	if !got.Enabled {
		t.Error("Enabled should be true")
	}
}

// ── buildCmds (pure, no kernel calls) ────────────────────────────────────────

func TestBuildCmds_MASQUERADE_NoSource(t *testing.T) {
	m := &Manager{am: nil}
	rule := &NatRule{
		OutInterface: "eth0",
		Type:         "MASQUERADE",
		Source:       "",
	}
	cmds, err := m.buildCmds(rule, "A")
	if err != nil {
		t.Fatalf("buildCmds: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 cmd, got %d", len(cmds))
	}
	cmd := cmds[0]
	expected := "iptables-nft -t nat -A POSTROUTING -o eth0 -j MASQUERADE"
	if cmd != expected {
		t.Errorf("buildCmds = %q, want %q", cmd, expected)
	}
}

func TestBuildCmds_MASQUERADE_WithSource(t *testing.T) {
	m := &Manager{am: nil}
	rule := &NatRule{
		OutInterface: "eth0",
		Type:         "MASQUERADE",
		Source:       "10.8.0.0/24",
	}
	cmds, err := m.buildCmds(rule, "A")
	if err != nil {
		t.Fatalf("buildCmds: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 cmd, got %d", len(cmds))
	}
	expected := "iptables-nft -t nat -A POSTROUTING -s 10.8.0.0/24 -o eth0 -j MASQUERADE"
	if cmds[0] != expected {
		t.Errorf("buildCmds = %q, want %q", cmds[0], expected)
	}
}

func TestBuildCmds_SNAT(t *testing.T) {
	m := &Manager{am: nil}
	rule := &NatRule{
		OutInterface: "eth0",
		Type:         "SNAT",
		ToSource:     "1.2.3.4",
		Source:       "10.0.0.0/8",
	}
	cmds, err := m.buildCmds(rule, "A")
	if err != nil {
		t.Fatalf("buildCmds: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 cmd, got %d", len(cmds))
	}
	expected := "iptables-nft -t nat -A POSTROUTING -s 10.0.0.0/8 -o eth0 -j SNAT --to-source 1.2.3.4"
	if cmds[0] != expected {
		t.Errorf("buildCmds = %q, want %q", cmds[0], expected)
	}
}

func TestBuildCmds_DeleteAction(t *testing.T) {
	m := &Manager{am: nil}
	rule := &NatRule{
		OutInterface: "wg10",
		Type:         "MASQUERADE",
	}
	cmds, err := m.buildCmds(rule, "D")
	if err != nil {
		t.Fatalf("buildCmds D: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 cmd, got %d", len(cmds))
	}
	expected := "iptables-nft -t nat -D POSTROUTING -o wg10 -j MASQUERADE"
	if cmds[0] != expected {
		t.Errorf("buildCmds D = %q, want %q", cmds[0], expected)
	}
}

// ── CRUD with DB ─────────────────────────────────────────────────────────────

func TestGetRules_EmptyOnFreshDB(t *testing.T) {
	m := initTestDB(t)
	rules, err := m.GetRules()
	if err != nil {
		t.Fatalf("GetRules: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules on fresh DB, got %d", len(rules))
	}
}

func TestGetRule_NotFound(t *testing.T) {
	m := initTestDB(t)
	rule, err := m.GetRule("no-such-id")
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if rule != nil {
		t.Errorf("expected nil for unknown ID, got %+v", rule)
	}
}
