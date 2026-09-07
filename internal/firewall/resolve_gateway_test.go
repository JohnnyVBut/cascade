package firewall

import (
	"testing"
	"time"

	"github.com/JohnnyVBut/cascade/internal/gateway"
)

func boolPtr(b bool) *bool { return &b }

// stubRouteExec replaces pbrRouteExec with a no-op success stub for the
// duration of the test, so triggerFallback/restoreRoute/failoverToNextTier
// can run their full state-transition logic without a real "ip" binary
// (unavailable outside a Linux container) or NET_ADMIN.
func stubRouteExec(t *testing.T) {
	t.Helper()
	orig := pbrRouteExec
	pbrRouteExec = func(cmd string, timeout time.Duration, log bool) (string, error) {
		return "", nil
	}
	t.Cleanup(func() { pbrRouteExec = orig })
}

func fwmarkPtr(v int) *int { return &v }

// newTestGroup creates two gateways (tier1, tier2) plus a group referencing
// both, returning their IDs for use in test rules.
func newTestGroup(t *testing.T, m *Manager) (groupID, gw1ID, gw2ID string) {
	t.Helper()
	gw1, err := m.gm.CreateGateway(gateway.GatewayInput{
		Name: "primary", Interface: "wg10", GatewayIP: "10.0.0.1",
		Enabled: boolPtr(true), Monitor: boolPtr(true), MonitorInterval: 3600,
	})
	if err != nil {
		t.Fatalf("CreateGateway gw1: %v", err)
	}
	gw2, err := m.gm.CreateGateway(gateway.GatewayInput{
		Name: "backup", Interface: "wg11", GatewayIP: "10.0.0.2",
		Enabled: boolPtr(true), Monitor: boolPtr(true), MonitorInterval: 3600,
	})
	if err != nil {
		t.Fatalf("CreateGateway gw2: %v", err)
	}
	grp, err := m.gm.CreateGroup(gateway.GatewayGroupInput{
		Name: "test-group",
		Gateways: []gateway.GatewayGroupMember{
			{GatewayID: gw1.ID, Tier: 1},
			{GatewayID: gw2.ID, Tier: 2},
		},
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	return grp.ID, gw1.ID, gw2.ID
}

// Regression test for GitHub issue #97: a PBR rule referencing a
// GatewayGroupID must fail over to the next healthy tier when the primary
// goes down, instead of always resolving to tier1 regardless of health.
func TestResolveGateway_GroupFailsOverToHealthyTier(t *testing.T) {
	m, _ := initTestDB(t)
	groupID, gw1ID, _ := newTestGroup(t, m)

	m.gm.Monitor().SetAdminDown(gw1ID, true)

	rule := &Rule{GatewayGroupID: groupID}
	resolved, err := m.resolveGateway(rule)
	if err != nil {
		t.Fatalf("resolveGateway: %v", err)
	}
	if resolved.gatewayIP != "10.0.0.2" || resolved.iface != "wg11" {
		t.Errorf("got (%s, %s), want backup gateway (10.0.0.2, wg11) — primary is down", resolved.gatewayIP, resolved.iface)
	}

	// Recovery: bring the primary back — must resolve to tier1 again.
	m.gm.Monitor().SetAdminDown(gw1ID, false)
	resolved, err = m.resolveGateway(rule)
	if err != nil {
		t.Fatalf("resolveGateway after recovery: %v", err)
	}
	if resolved.gatewayIP != "10.0.0.1" || resolved.iface != "wg10" {
		t.Errorf("got (%s, %s), want primary gateway (10.0.0.1, wg10) after recovery", resolved.gatewayIP, resolved.iface)
	}
}

func TestGroupContainsGateway(t *testing.T) {
	m, _ := initTestDB(t)
	groupID, gw1ID, _ := newTestGroup(t, m)

	if !m.gm.GroupContainsGateway(groupID, gw1ID) {
		t.Error("expected group to contain gw1")
	}
	if m.gm.GroupContainsGateway(groupID, "no-such-gateway") {
		t.Error("expected group to not contain an unrelated gateway ID")
	}
	if m.gm.GroupContainsGateway("no-such-group", gw1ID) {
		t.Error("expected unknown group to return false, not error out")
	}
}

// ── End-to-end onGatewayDown/onGatewayUp state machine ─────────────────────
// These exercise the actual issue #97 fix: the GatewayMonitor callback path,
// not just the resolver it calls. pbrRouteExec is stubbed so the test runs
// without a real "ip" binary.

func TestOnGatewayDown_PartialGroupFailure_FailsOverWithoutBlackhole(t *testing.T) {
	m, _ := initTestDB(t)
	groupID, gw1ID, _ := newTestGroup(t, m)
	stubRouteExec(t)

	rule := Rule{ID: "rule-1", Enabled: true, GatewayGroupID: groupID, Fwmark: fwmarkPtr(100)}
	if err := insertRule(rule); err != nil {
		t.Fatalf("insertRule: %v", err)
	}

	// Only the primary goes down — the group is NOT fully down (backup is
	// still healthy/unknown), so this must NOT blackhole/fallback-to-default.
	m.gm.Monitor().SetAdminDown(gw1ID, true)
	if err := m.onGatewayDown(gw1ID); err != nil {
		t.Fatalf("onGatewayDown: %v", err)
	}

	m.fallbackMu.Lock()
	active := m.fallbackActive[rule.ID]
	m.fallbackMu.Unlock()
	if active {
		t.Error("rule should NOT be in fallback/blackhole — the group still has a healthy tier2")
	}

	// The route must now point at the backup, not the dead primary.
	resolved, err := m.resolveGateway(&rule)
	if err != nil {
		t.Fatalf("resolveGateway: %v", err)
	}
	if resolved.gatewayIP != "10.0.0.2" {
		t.Errorf("resolveGateway = %s, want backup 10.0.0.2", resolved.gatewayIP)
	}
}

func TestOnGatewayDown_AllDown_TriggersBlackhole(t *testing.T) {
	m, _ := initTestDB(t)
	groupID, gw1ID, gw2ID := newTestGroup(t, m)
	stubRouteExec(t)

	rule := Rule{ID: "rule-1", Enabled: true, GatewayGroupID: groupID, Fwmark: fwmarkPtr(100), FallbackToDefault: false}
	if err := insertRule(rule); err != nil {
		t.Fatalf("insertRule: %v", err)
	}

	m.gm.Monitor().SetAdminDown(gw1ID, true)
	m.gm.Monitor().SetAdminDown(gw2ID, true)
	if err := m.onGatewayDown(gw2ID); err != nil {
		t.Fatalf("onGatewayDown: %v", err)
	}

	m.fallbackMu.Lock()
	active := m.fallbackActive[rule.ID]
	m.fallbackMu.Unlock()
	if !active {
		t.Error("rule should be in fallback/blackhole — every member of the group is down")
	}
}

// Recovered tier1 must reclaim priority even though the rule was only
// partially failed-over (never fully blackholed) — the gap onGatewayUp had
// before this fix.
func TestOnGatewayUp_RecoveredPrimaryReclaimsPriorityWithoutFullFallback(t *testing.T) {
	m, _ := initTestDB(t)
	groupID, gw1ID, _ := newTestGroup(t, m)
	stubRouteExec(t)

	rule := Rule{ID: "rule-1", Enabled: true, GatewayGroupID: groupID, Fwmark: fwmarkPtr(100)}
	if err := insertRule(rule); err != nil {
		t.Fatalf("insertRule: %v", err)
	}

	m.gm.Monitor().SetAdminDown(gw1ID, true)
	if err := m.onGatewayDown(gw1ID); err != nil {
		t.Fatalf("onGatewayDown: %v", err)
	}
	// Sanity: confirm we actually failed over (not blackholed) before recovery.
	m.fallbackMu.Lock()
	activeBefore := m.fallbackActive[rule.ID]
	m.fallbackMu.Unlock()
	if activeBefore {
		t.Fatal("precondition failed: rule should not be in fallback before recovery")
	}

	m.gm.Monitor().SetAdminDown(gw1ID, false)
	if err := m.onGatewayUp(gw1ID); err != nil {
		t.Fatalf("onGatewayUp: %v", err)
	}

	// onGatewayUp schedules the actual restore via a 30s anti-flap timer, so
	// we can't observe the applied route synchronously here — but resolveGateway
	// itself (used by the eventual restoreRoute call) must already report tier1
	// as the answer, confirming the resolver side of the recovery is correct.
	resolved, err := m.resolveGateway(&rule)
	if err != nil {
		t.Fatalf("resolveGateway: %v", err)
	}
	if resolved.gatewayIP != "10.0.0.1" {
		t.Errorf("resolveGateway = %s, want primary 10.0.0.1 after recovery", resolved.gatewayIP)
	}

	m.fallbackMu.Lock()
	_, scheduled := m.restoreTimers[rule.ID]
	m.fallbackMu.Unlock()
	if !scheduled {
		t.Error("expected onGatewayUp to schedule a restore timer for the recovered rule")
	}
}

// Two PBR rules referencing the same gateway group must fail over and
// recover independently — fallbackActive/restoreTimers are keyed by rule ID.
func TestOnGatewayDown_MultipleRulesShareOneGroup(t *testing.T) {
	m, _ := initTestDB(t)
	groupID, gw1ID, _ := newTestGroup(t, m)
	stubRouteExec(t)

	ruleA := Rule{ID: "rule-a", Enabled: true, GatewayGroupID: groupID, Fwmark: fwmarkPtr(100)}
	ruleB := Rule{ID: "rule-b", Enabled: true, GatewayGroupID: groupID, Fwmark: fwmarkPtr(200)}
	ruleDisabled := Rule{ID: "rule-c", Enabled: false, GatewayGroupID: groupID, Fwmark: fwmarkPtr(300)}
	for _, r := range []Rule{ruleA, ruleB, ruleDisabled} {
		if err := insertRule(r); err != nil {
			t.Fatalf("insertRule %s: %v", r.ID, err)
		}
	}

	m.gm.Monitor().SetAdminDown(gw1ID, true)
	if err := m.onGatewayDown(gw1ID); err != nil {
		t.Fatalf("onGatewayDown: %v", err)
	}

	for _, id := range []string{"rule-a", "rule-b"} {
		resolved, err := m.resolveGateway(&Rule{GatewayGroupID: groupID})
		if err != nil {
			t.Fatalf("resolveGateway for %s: %v", id, err)
		}
		if resolved.gatewayIP != "10.0.0.2" {
			t.Errorf("%s: resolveGateway = %s, want backup 10.0.0.2", id, resolved.gatewayIP)
		}
		m.fallbackMu.Lock()
		active := m.fallbackActive[id]
		m.fallbackMu.Unlock()
		if active {
			t.Errorf("%s: should not be blackholed — group still has a healthy tier2", id)
		}
	}

	// The disabled rule must be left untouched entirely (onGatewayDown skips
	// !rule.Enabled rules) — nothing to assert beyond "no panic/error above".
}
