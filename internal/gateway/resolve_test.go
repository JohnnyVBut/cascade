package gateway

import (
	"os"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
)

// setStatus injects a monitor status directly into the Monitor's internal
// state, bypassing real ICMP/HTTP probes — keeps these tests fast and
// deterministic (no network/exec dependency).
func setStatus(mon *Monitor, gatewayID, status string) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	mon.states[gatewayID] = &monitorState{
		status: MonitorStatus{Status: status},
		stopCh: make(chan struct{}),
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir, err := os.MkdirTemp("", "cascade-gateway-test-*")
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
	return NewManager()
}

// insertTestGateway writes a gateway row directly (bypassing CreateGateway's
// monitor.Start/ensureHostRoute side effects, which shell out to ping/ip and
// are irrelevant to the tier-resolution logic under test).
func insertTestGateway(t *testing.T, m *Manager, id, ip, iface string) {
	t.Helper()
	gw := Gateway{
		ID:        id,
		Name:      id,
		Interface: iface,
		GatewayIP: ip,
		Enabled:   true,
	}
	if err := insertGateway(gw); err != nil {
		t.Fatalf("insertGateway(%s): %v", id, err)
	}
}

func TestResolveGroupGateway_PicksLowestHealthyTier(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")
	insertTestGateway(t, m, "gw2", "10.0.0.2", "wg11")

	grp, err := m.CreateGroup(GatewayGroupInput{
		Name: "test-group",
		Gateways: []GatewayGroupMember{
			{GatewayID: "gw1", Tier: 1},
			{GatewayID: "gw2", Tier: 2},
		},
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	setStatus(m.Monitor(), "gw1", "healthy")
	setStatus(m.Monitor(), "gw2", "healthy")

	ip, iface, err := m.ResolveGroupGateway(grp.ID)
	if err != nil {
		t.Fatalf("ResolveGroupGateway: %v", err)
	}
	if ip != "10.0.0.1" || iface != "wg10" {
		t.Errorf("got (%s, %s), want tier1 gateway (10.0.0.1, wg10)", ip, iface)
	}
}

// This is the exact regression covered by GitHub issue #97: primary (tier1)
// down, backup (tier2) healthy — must fail over to tier2, not stay pinned to
// the dead tier1 and not blackhole either (that's reserved for "all down").
func TestResolveGroupGateway_FailsOverWhenPrimaryDown(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")
	insertTestGateway(t, m, "gw2", "10.0.0.2", "wg11")

	grp, err := m.CreateGroup(GatewayGroupInput{
		Name: "test-group",
		Gateways: []GatewayGroupMember{
			{GatewayID: "gw1", Tier: 1},
			{GatewayID: "gw2", Tier: 2},
		},
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	setStatus(m.Monitor(), "gw1", "down")
	setStatus(m.Monitor(), "gw2", "healthy")

	ip, iface, err := m.ResolveGroupGateway(grp.ID)
	if err != nil {
		t.Fatalf("ResolveGroupGateway: %v", err)
	}
	if ip != "10.0.0.2" || iface != "wg11" {
		t.Errorf("got (%s, %s), want tier2 fallback (10.0.0.2, wg11)", ip, iface)
	}
}

func TestResolveGroupGateway_AllDownFallsBackToTier1(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")
	insertTestGateway(t, m, "gw2", "10.0.0.2", "wg11")

	grp, err := m.CreateGroup(GatewayGroupInput{
		Name: "test-group",
		Gateways: []GatewayGroupMember{
			{GatewayID: "gw1", Tier: 1},
			{GatewayID: "gw2", Tier: 2},
		},
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	setStatus(m.Monitor(), "gw1", "down")
	setStatus(m.Monitor(), "gw2", "admin_down")

	ip, iface, err := m.ResolveGroupGateway(grp.ID)
	if err != nil {
		t.Fatalf("ResolveGroupGateway: %v", err)
	}
	if ip != "10.0.0.1" || iface != "wg10" {
		t.Errorf("got (%s, %s), want tier1 as gateway of last resort (10.0.0.1, wg10)", ip, iface)
	}
}

func TestResolveGroupGateway_UnknownStatusTreatedAsAvailable(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")

	grp, err := m.CreateGroup(GatewayGroupInput{
		Name:     "test-group",
		Gateways: []GatewayGroupMember{{GatewayID: "gw1", Tier: 1}},
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// No setStatus call — GetStatus() returns "unknown" for an unmonitored gateway.
	ip, iface, err := m.ResolveGroupGateway(grp.ID)
	if err != nil {
		t.Fatalf("ResolveGroupGateway: %v", err)
	}
	if ip != "10.0.0.1" || iface != "wg10" {
		t.Errorf("got (%s, %s), want (10.0.0.1, wg10)", ip, iface)
	}
}

// Exercises the multi-tier walk beyond the 2-tier cases above: tier1 and
// tier2 both down, tier3 healthy — must skip past both dead tiers to tier3,
// not just try the next one and stop.
func TestResolveGroupGateway_ThreeTiers_SkipsPastMultipleDeadTiers(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")
	insertTestGateway(t, m, "gw2", "10.0.0.2", "wg11")
	insertTestGateway(t, m, "gw3", "10.0.0.3", "wg12")

	grp, err := m.CreateGroup(GatewayGroupInput{
		Name: "test-group",
		Gateways: []GatewayGroupMember{
			{GatewayID: "gw1", Tier: 1},
			{GatewayID: "gw2", Tier: 2},
			{GatewayID: "gw3", Tier: 3},
		},
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	setStatus(m.Monitor(), "gw1", "down")
	setStatus(m.Monitor(), "gw2", "down")
	setStatus(m.Monitor(), "gw3", "healthy")

	ip, iface, err := m.ResolveGroupGateway(grp.ID)
	if err != nil {
		t.Fatalf("ResolveGroupGateway: %v", err)
	}
	if ip != "10.0.0.3" || iface != "wg12" {
		t.Errorf("got (%s, %s), want tier3 (10.0.0.3, wg12) — both tier1 and tier2 are down", ip, iface)
	}
}

// Two gateways share the same tier (weight-balanced pair): one down, one
// healthy. The healthy sibling in the SAME tier must be picked before ever
// falling through to a lower-priority tier.
func TestResolveGroupGateway_SameTier_PicksHealthySibling(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1a", "10.0.0.1", "wg10")
	insertTestGateway(t, m, "gw1b", "10.0.0.2", "wg11")
	insertTestGateway(t, m, "gw2", "10.0.0.3", "wg12")

	grp, err := m.CreateGroup(GatewayGroupInput{
		Name: "test-group",
		Gateways: []GatewayGroupMember{
			{GatewayID: "gw1a", Tier: 1, Weight: 1},
			{GatewayID: "gw1b", Tier: 1, Weight: 1},
			{GatewayID: "gw2", Tier: 2},
		},
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	setStatus(m.Monitor(), "gw1a", "down")
	setStatus(m.Monitor(), "gw1b", "healthy")
	setStatus(m.Monitor(), "gw2", "healthy")

	ip, _, err := m.ResolveGroupGateway(grp.ID)
	if err != nil {
		t.Fatalf("ResolveGroupGateway: %v", err)
	}
	if ip != "10.0.0.2" {
		t.Errorf("got %s, want tier1 sibling gw1b (10.0.0.2) — should not fall through to tier2 while a tier1 member is healthy", ip)
	}
}
