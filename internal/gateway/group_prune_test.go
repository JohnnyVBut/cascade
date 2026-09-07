// Regression tests for issue #106: deleting a gateway left a "ghost" entry
// behind in any gateway_groups.members list it belonged to — surviving even
// the v0.9.7 update, since that release's #96 fix only pruned stale
// references from dashboard_widgets, a different storage location for the
// same class of dangling-reference bug.
package gateway

import (
	"encoding/json"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
)

// TestDeleteGateway_PrunesGhostGroupMember covers the forward-fix: deleting
// a gateway that's a member of a group must remove it from that group's
// members list in the same transaction as the delete.
func TestDeleteGateway_PrunesGhostGroupMember(t *testing.T) {
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

	if err := m.DeleteGateway("gw1"); err != nil {
		t.Fatalf("DeleteGateway: %v", err)
	}

	got, err := m.GetGroup(grp.ID)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if got == nil {
		t.Fatal("group unexpectedly gone")
	}
	if len(got.Gateways) != 1 || got.Gateways[0].GatewayID != "gw2" {
		t.Fatalf("group members after delete = %+v, want only gw2", got.Gateways)
	}
}

// TestDeleteGateway_PrunesGhostFromMultipleGroups covers a gateway that's a
// member of more than one group at once — all of them must be pruned in the
// same DeleteGateway call, not just the first one found.
func TestDeleteGateway_PrunesGhostFromMultipleGroups(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")
	insertTestGateway(t, m, "gw2", "10.0.0.2", "wg11")

	g1, err := m.CreateGroup(GatewayGroupInput{
		Name:     "group-a",
		Gateways: []GatewayGroupMember{{GatewayID: "gw1", Tier: 1}, {GatewayID: "gw2", Tier: 2}},
	})
	if err != nil {
		t.Fatalf("CreateGroup a: %v", err)
	}
	g2, err := m.CreateGroup(GatewayGroupInput{
		Name:     "group-b",
		Gateways: []GatewayGroupMember{{GatewayID: "gw1", Tier: 1}},
	})
	if err != nil {
		t.Fatalf("CreateGroup b: %v", err)
	}

	if err := m.DeleteGateway("gw1"); err != nil {
		t.Fatalf("DeleteGateway: %v", err)
	}

	gotA, _ := m.GetGroup(g1.ID)
	if len(gotA.Gateways) != 1 || gotA.Gateways[0].GatewayID != "gw2" {
		t.Errorf("group-a members = %+v, want only gw2", gotA.Gateways)
	}
	gotB, _ := m.GetGroup(g2.ID)
	if len(gotB.Gateways) != 0 {
		t.Errorf("group-b members = %+v, want empty", gotB.Gateways)
	}
}

// TestInit_SelfHealsPreExistingGhostGroupMember covers the case the forward
// fix above cannot: a ghost member already sitting in gateway_groups.members
// from before this fix shipped (or written by any other path). No user
// action should be required — Init() (called once at process startup) must
// clean it up on its own.
func TestInit_SelfHealsPreExistingGhostGroupMember(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")

	// Simulate a pre-existing ghost: a group whose members reference "gw-ghost",
	// which was never actually inserted into the gateways table (as if it had
	// been deleted by the old, buggy DeleteGateway before this fix).
	members, _ := json.Marshal([]GatewayGroupMember{
		{GatewayID: "gw1", Tier: 1},
		{GatewayID: "gw-ghost", Tier: 2},
	})
	if _, err := insertTestGroupRaw(t, "grp1", "test-group", string(members)); err != nil {
		t.Fatalf("insertTestGroupRaw: %v", err)
	}

	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	got, err := m.GetGroup("grp1")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if len(got.Gateways) != 1 || got.Gateways[0].GatewayID != "gw1" {
		t.Fatalf("group members after Init self-heal = %+v, want only gw1", got.Gateways)
	}
}

// TestInit_SelfHeal_LeavesHealthyGroupsUntouched is the negative case: a
// group with no ghost members must survive Init()'s self-heal unchanged
// (proves the fix doesn't touch — or accidentally corrupt — groups that
// don't need healing).
func TestInit_SelfHeal_LeavesHealthyGroupsUntouched(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")
	insertTestGateway(t, m, "gw2", "10.0.0.2", "wg11")

	grp, err := m.CreateGroup(GatewayGroupInput{
		Name:     "healthy-group",
		Gateways: []GatewayGroupMember{{GatewayID: "gw1", Tier: 1}, {GatewayID: "gw2", Tier: 2}},
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	got, err := m.GetGroup(grp.ID)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if len(got.Gateways) != 2 {
		t.Fatalf("healthy group members after Init = %+v, want unchanged 2 members", got.Gateways)
	}
}

// insertTestGroupRaw writes a gateway_groups row directly with a caller-
// supplied members JSON blob — bypassing CreateGroup's validation, which
// would otherwise reject a member referencing a gateway ID that doesn't
// exist in the gateways table (exactly the ghost state this test needs to
// construct).
func insertTestGroupRaw(t *testing.T, id, name, membersJSON string) (string, error) {
	t.Helper()
	_, err := db.DB().Exec(
		`INSERT INTO gateway_groups (id, name, trigger, description, members, created_at) VALUES (?, ?, ?, ?, ?, datetime('now'))`,
		id, name, "packetloss", "", membersJSON,
	)
	return id, err
}
