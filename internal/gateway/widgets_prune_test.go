package gateway

import (
	"encoding/json"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
)

// insertTestWidgetRow writes a dashboard_widgets row directly, bypassing any
// dashboard package API (not needed here — we only care about the raw JSON
// column that pruneDashboardWidgetsForGateway reads/writes).
func insertTestWidgetRow(t *testing.T, userID, page, widgetsJSON string) {
	t.Helper()
	if _, err := db.DB().Exec(
		`INSERT INTO dashboard_widgets (user_id, page, widgets) VALUES (?, ?, ?)`,
		userID, page, widgetsJSON,
	); err != nil {
		t.Fatalf("insertTestWidgetRow(%s/%s): %v", userID, page, err)
	}
}

// readTestWidgetRow reads back the raw widgets JSON column for a given row.
func readTestWidgetRow(t *testing.T, userID, page string) string {
	t.Helper()
	var widgetsJSON string
	if err := db.DB().QueryRow(
		`SELECT widgets FROM dashboard_widgets WHERE user_id = ? AND page = ?`,
		userID, page,
	).Scan(&widgetsJSON); err != nil {
		t.Fatalf("readTestWidgetRow(%s/%s): %v", userID, page, err)
	}
	return widgetsJSON
}

func TestDeleteGateway_PrunesStaleGraphReference(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")

	widgets := `[{"id":"w1","type":"chart","graphs":["gateway:gw1","net:wg0:rx"]}]`
	insertTestWidgetRow(t, "user1", "dashboard", widgets)

	if err := m.DeleteGateway("gw1"); err != nil {
		t.Fatalf("DeleteGateway: %v", err)
	}

	got := readTestWidgetRow(t, "user1", "dashboard")

	var parsed []map[string]interface{}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("unmarshal updated widgets: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(parsed))
	}
	graphs, ok := parsed[0]["graphs"].([]interface{})
	if !ok {
		t.Fatalf("graphs field missing or wrong type: %v", parsed[0]["graphs"])
	}
	for _, g := range graphs {
		if g == "gateway:gw1" {
			t.Errorf("stale gateway:gw1 reference still present in graphs: %v", graphs)
		}
	}
	found := false
	for _, g := range graphs {
		if g == "net:wg0:rx" {
			found = true
		}
	}
	if !found {
		t.Errorf("unrelated graph net:wg0:rx should be preserved, got: %v", graphs)
	}
}

func TestDeleteGateway_LeavesOtherGatewayReferencesUntouched(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")
	insertTestGateway(t, m, "gw2", "10.0.0.2", "wg11")

	widgets := `[{"id":"w1","type":"chart","graphs":["gateway:gw2","net:wg0:rx"]}]`
	insertTestWidgetRow(t, "user1", "dashboard", widgets)

	if err := m.DeleteGateway("gw1"); err != nil {
		t.Fatalf("DeleteGateway: %v", err)
	}

	got := readTestWidgetRow(t, "user1", "dashboard")
	if got != widgets {
		t.Errorf("expected untouched row for unrelated gateway deletion, got %q, want %q", got, widgets)
	}
}

func TestDeleteGateway_PrunesGraphColors(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")

	widgets := `[{"id":"w1","type":"chart","graphs":["gateway:gw1"],"graphColors":{"gateway:gw1":"#ff0000","net:wg0:rx":"#00ff00"}}]`
	insertTestWidgetRow(t, "user1", "dashboard", widgets)

	if err := m.DeleteGateway("gw1"); err != nil {
		t.Fatalf("DeleteGateway: %v", err)
	}

	got := readTestWidgetRow(t, "user1", "dashboard")
	var parsed []map[string]interface{}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("unmarshal updated widgets: %v", err)
	}
	colors, ok := parsed[0]["graphColors"].(map[string]interface{})
	if !ok {
		t.Fatalf("graphColors field missing or wrong type: %v", parsed[0]["graphColors"])
	}
	if _, exists := colors["gateway:gw1"]; exists {
		t.Errorf("stale graphColors[gateway:gw1] entry still present: %v", colors)
	}
	if _, exists := colors["net:wg0:rx"]; !exists {
		t.Errorf("unrelated graphColors entry net:wg0:rx should be preserved, got: %v", colors)
	}
}

func TestDeleteGateway_RowWithNoMatchingGraphKeyStaysCorrect(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")

	widgets := `[{"id":"w1","type":"chart","graphs":["net:wg0:rx","net:wg0:tx"]}]`
	insertTestWidgetRow(t, "user1", "dashboard", widgets)

	if err := m.DeleteGateway("gw1"); err != nil {
		t.Fatalf("DeleteGateway: %v", err)
	}

	got := readTestWidgetRow(t, "user1", "dashboard")

	var gotParsed, wantParsed []map[string]interface{}
	if err := json.Unmarshal([]byte(got), &gotParsed); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if err := json.Unmarshal([]byte(widgets), &wantParsed); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	gotJSON, _ := json.Marshal(gotParsed)
	wantJSON, _ := json.Marshal(wantParsed)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("row content changed unexpectedly:\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
}

func TestDeleteGateway_PrunesAllUsersRows(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")

	insertTestWidgetRow(t, "user1", "dashboard", `[{"id":"w1","graphs":["gateway:gw1"]}]`)
	insertTestWidgetRow(t, "user2", "dashboard", `[{"id":"w2","graphs":["gateway:gw1","net:wg0:rx"]}]`)
	insertTestWidgetRow(t, "user2", "diagnostics", `[{"id":"w3","graphs":["gateway:gw1"]}]`)
	insertTestWidgetRow(t, "user3", "dashboard", `[{"id":"w4","graphs":["net:wg0:rx"]}]`)

	if err := m.DeleteGateway("gw1"); err != nil {
		t.Fatalf("DeleteGateway: %v", err)
	}

	for _, tc := range []struct{ userID, page string }{
		{"user1", "dashboard"},
		{"user2", "dashboard"},
		{"user2", "diagnostics"},
	} {
		got := readTestWidgetRow(t, tc.userID, tc.page)
		var parsed []map[string]interface{}
		if err := json.Unmarshal([]byte(got), &parsed); err != nil {
			t.Fatalf("unmarshal %s/%s: %v", tc.userID, tc.page, err)
		}
		graphs, _ := parsed[0]["graphs"].([]interface{})
		for _, g := range graphs {
			if g == "gateway:gw1" {
				t.Errorf("%s/%s: stale gateway:gw1 reference still present: %v", tc.userID, tc.page, graphs)
			}
		}
	}

	// user3's unrelated row must stay untouched.
	got := readTestWidgetRow(t, "user3", "dashboard")
	want := `[{"id":"w4","graphs":["net:wg0:rx"]}]`
	if got != want {
		t.Errorf("user3/dashboard row changed unexpectedly: got %q, want %q", got, want)
	}
}

func TestDeleteGateway_MalformedWidgetsJSONDoesNotCrashPruning(t *testing.T) {
	m := newTestManager(t)
	insertTestGateway(t, m, "gw1", "10.0.0.1", "wg10")

	// Malformed JSON row — must be skipped gracefully, not crash pruning.
	insertTestWidgetRow(t, "userBad", "dashboard", `{not valid json`)
	// A valid row referencing the deleted gateway must still be pruned correctly.
	insertTestWidgetRow(t, "userGood", "dashboard", `[{"id":"w1","graphs":["gateway:gw1","net:wg0:rx"]}]`)

	if err := m.DeleteGateway("gw1"); err != nil {
		t.Fatalf("DeleteGateway: %v", err)
	}

	// Malformed row is left as-is (skipped), not crashed on.
	gotBad := readTestWidgetRow(t, "userBad", "dashboard")
	if gotBad != `{not valid json` {
		t.Errorf("malformed row should be left untouched, got %q", gotBad)
	}

	gotGood := readTestWidgetRow(t, "userGood", "dashboard")
	var parsed []map[string]interface{}
	if err := json.Unmarshal([]byte(gotGood), &parsed); err != nil {
		t.Fatalf("unmarshal userGood: %v", err)
	}
	graphs, _ := parsed[0]["graphs"].([]interface{})
	for _, g := range graphs {
		if g == "gateway:gw1" {
			t.Errorf("userGood: stale gateway:gw1 reference still present: %v", graphs)
		}
	}
}
