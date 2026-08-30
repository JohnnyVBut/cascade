// Package api — tests for GET/PUT /api/dashboard/widgets, in particular the
// self-healing stale "gateway:<id>" reference pruning added in
// getDashboardWidgets (see pruneStaleGatewayRefsFromWidgetsJSON).
//
// GitHub issue #96: deleted gateways left stale "gateway:<id>" refs in saved
// Diagnostics/Dashboard widgets forever, because the original cleanup
// (pruneDashboardWidgetsForGateway in internal/gateway/manager.go) only runs
// going forward from the moment a gateway is deleted — rows written before
// that fix (or created through some other path) never got cleaned up. These
// tests exercise the read-time self-heal added in getDashboardWidgets.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/gateway"
	"github.com/JohnnyVBut/cascade/internal/tokens"
	"github.com/JohnnyVBut/cascade/internal/users"
)

// ── Harness ───────────────────────────────────────────────────────────────────

// dashboardTestApp is a minimal Fiber application with the dashboard routes
// registered behind AuthMiddleware, following the same pattern as
// settingsTestApp in settings_test.go.
type dashboardTestApp struct {
	app    *fiber.App
	userID string
	token  string // raw Bearer token for the test user
}

func newDashboardTestApp(t *testing.T) *dashboardTestApp {
	t.Helper()

	dir := t.TempDir()
	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	// db's instance is a single unguarded package-level global shared by the
	// whole test binary (see import_client_configs_test.go's TestMain) —
	// closing it here would leave every test that runs afterward with a nil
	// db.DB(), and re-opening a fresh empty one would leave them pointed at
	// a database missing the fixtures TestMain seeded. Restore the shared
	// TestMain database rather than just closing this test's own.
	t.Cleanup(func() {
		db.Close()
		if sharedTestDBDir != "" {
			if err := db.Init(sharedTestDBDir); err != nil {
				t.Fatalf("restore shared test db: %v", err)
			}
		}
	})

	// getDashboardWidgets calls gateway.Get(), which panics unless a Manager
	// singleton has been installed (see gateway.SetInstance).
	gateway.SetInstance(gateway.NewManager())

	InitAuth("") // initialise session store

	u, err := users.Create("dashuser", "dashpass1")
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	_, rawToken, err := tokens.Create(u.ID, "dashboard-test-token")
	if err != nil {
		t.Fatalf("tokens.Create: %v", err)
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			msg := err.Error()
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
				msg = e.Message
			}
			return c.Status(code).JSON(fiber.Map{"error": msg})
		},
	})

	api := app.Group("/api", AuthMiddleware)
	RegisterDashboard(api)

	return &dashboardTestApp{app: app, userID: u.ID, token: rawToken}
}

// get is a convenience wrapper: GET /api/dashboard/widgets?page=<page>.
func (dta *dashboardTestApp) get(t *testing.T, page string) *http.Response {
	t.Helper()
	ta := &testApp{app: dta.app, adminToken: dta.token}
	return ta.do("GET", "/api/dashboard/widgets?page="+page, dta.token, nil)
}

// insertGatewayRow inserts a minimal live gateway row directly — all other
// columns have NOT NULL DEFAULT values, so id/name is enough for
// gateway.GetGateways() to return a working Gateway.
func insertGatewayRow(t *testing.T, id string) {
	t.Helper()
	if _, err := db.DB().Exec(`INSERT INTO gateways (id, name) VALUES (?, ?)`, id, id); err != nil {
		t.Fatalf("insertGatewayRow(%s): %v", id, err)
	}
}

// insertWidgetsRow writes a dashboard_widgets row directly, bypassing
// PUT /api/dashboard/widgets — we only care about the raw JSON column that
// getDashboardWidgets reads/prunes/writes back.
func insertWidgetsRow(t *testing.T, userID, page, widgetsJSON string) {
	t.Helper()
	if _, err := db.DB().Exec(
		`INSERT INTO dashboard_widgets (user_id, page, widgets) VALUES (?, ?, ?)`,
		userID, page, widgetsJSON,
	); err != nil {
		t.Fatalf("insertWidgetsRow(%s/%s): %v", userID, page, err)
	}
}

// readWidgetsRow reads back the raw widgets JSON column for a given row.
// Returns ("", false) if no row exists.
func readWidgetsRow(t *testing.T, userID, page string) (string, bool) {
	t.Helper()
	var widgetsJSON string
	err := db.DB().QueryRow(
		`SELECT widgets FROM dashboard_widgets WHERE user_id = ? AND page = ?`,
		userID, page,
	).Scan(&widgetsJSON)
	if err != nil {
		return "", false
	}
	return widgetsJSON, true
}

// ── GET /dashboard/widgets — self-heal stale gateway refs ───────────────────

func TestGetDashboardWidgets_PrunesStaleGatewayRef(t *testing.T) {
	dta := newDashboardTestApp(t)
	// No live gateways — "gateway:gw-deleted" is stale.
	widgets := `[{"id":"w1","type":"chart","graphs":["gateway:gw-deleted","net:wg0:rx"]}]`
	insertWidgetsRow(t, dta.userID, "dashboard", widgets)

	resp := dta.get(t, "dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeBody(resp)
	respJSON := mustMarshal(t, body["widgets"])
	if strings.Contains(respJSON, "gateway:gw-deleted") {
		t.Errorf("response still contains stale gateway ref: %s", respJSON)
	}
	if !strings.Contains(respJSON, "net:wg0:rx") {
		t.Errorf("response lost unrelated graph ref: %s", respJSON)
	}

	// DB row must also be updated (persisted self-heal).
	got, ok := readWidgetsRow(t, dta.userID, "dashboard")
	if !ok {
		t.Fatal("expected widgets row to still exist")
	}
	if strings.Contains(got, "gateway:gw-deleted") {
		t.Errorf("DB row still contains stale gateway ref after self-heal: %s", got)
	}
}

func TestGetDashboardWidgets_LiveGatewayRefUntouched(t *testing.T) {
	dta := newDashboardTestApp(t)
	insertGatewayRow(t, "gw1")
	widgets := `[{"id":"w1","type":"chart","graphs":["gateway:gw1","net:wg0:rx"]}]`
	insertWidgetsRow(t, dta.userID, "dashboard", widgets)

	resp := dta.get(t, "dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeBody(resp)
	respJSON := mustMarshal(t, body["widgets"])
	if !strings.Contains(respJSON, "gateway:gw1") {
		t.Errorf("response should still contain live gateway ref: %s", respJSON)
	}

	// No unnecessary write: DB row must remain byte-identical to the seeded value.
	got, ok := readWidgetsRow(t, dta.userID, "dashboard")
	if !ok {
		t.Fatal("expected widgets row to still exist")
	}
	if got != widgets {
		t.Errorf("DB row changed unexpectedly for all-live widgets:\ngot:  %s\nwant: %s", got, widgets)
	}
}

func TestGetDashboardWidgets_MixedRefs_OnlyStaleGatewayRemoved(t *testing.T) {
	dta := newDashboardTestApp(t)
	insertGatewayRow(t, "gw-live")
	widgets := `[{"id":"w1","type":"chart","graphs":["gateway:gw-live","gateway:gw-gone","net:wg0:rx","cpu","mem"]}]`
	insertWidgetsRow(t, dta.userID, "dashboard", widgets)

	resp := dta.get(t, "dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeBody(resp)
	respJSON := mustMarshal(t, body["widgets"])

	if strings.Contains(respJSON, "gateway:gw-gone") {
		t.Errorf("stale gateway ref not removed: %s", respJSON)
	}
	for _, want := range []string{"gateway:gw-live", "net:wg0:rx", "cpu", "mem"} {
		if !strings.Contains(respJSON, want) {
			t.Errorf("expected %q preserved in response, got: %s", want, respJSON)
		}
	}

	got, ok := readWidgetsRow(t, dta.userID, "dashboard")
	if !ok {
		t.Fatal("expected widgets row to still exist")
	}
	if strings.Contains(got, "gateway:gw-gone") {
		t.Errorf("DB row still has stale ref: %s", got)
	}
	for _, want := range []string{"gateway:gw-live", "net:wg0:rx", "cpu", "mem"} {
		if !strings.Contains(got, want) {
			t.Errorf("DB row lost %q after self-heal: %s", want, got)
		}
	}
}

func TestGetDashboardWidgets_GraphColorsStaleKeyRemoved(t *testing.T) {
	dta := newDashboardTestApp(t)
	widgets := `[{"id":"w1","type":"chart","graphs":["gateway:gw-gone"],"graphColors":{"gateway:gw-gone":"#ff0000","net:wg0:rx":"#00ff00"}}]`
	insertWidgetsRow(t, dta.userID, "dashboard", widgets)

	resp := dta.get(t, "dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeBody(resp)
	respJSON := mustMarshal(t, body["widgets"])

	if strings.Contains(respJSON, `"gateway:gw-gone":"#ff0000"`) {
		t.Errorf("stale graphColors entry not removed: %s", respJSON)
	}
	if !strings.Contains(respJSON, `"net:wg0:rx":"#00ff00"`) {
		t.Errorf("unrelated graphColors entry should be preserved: %s", respJSON)
	}

	got, ok := readWidgetsRow(t, dta.userID, "dashboard")
	if !ok {
		t.Fatal("expected widgets row to still exist")
	}
	if strings.Contains(got, `"gateway:gw-gone":"#ff0000"`) {
		t.Errorf("DB row still has stale graphColors entry: %s", got)
	}
}

func TestGetDashboardWidgets_EmptyArrayNoRowWrite(t *testing.T) {
	dta := newDashboardTestApp(t)
	insertWidgetsRow(t, dta.userID, "dashboard", "[]")

	resp := dta.get(t, "dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeBody(resp)
	widgets, ok := body["widgets"].([]interface{})
	if !ok {
		t.Fatalf("expected widgets to be an array, got %T: %v", body["widgets"], body["widgets"])
	}
	if len(widgets) != 0 {
		t.Errorf("expected empty widgets array, got %v", widgets)
	}

	got, ok := readWidgetsRow(t, dta.userID, "dashboard")
	if !ok {
		t.Fatal("expected widgets row to still exist")
	}
	if got != "[]" {
		t.Errorf("DB row should remain untouched '[]', got %q", got)
	}
}

func TestGetDashboardWidgets_NoRowReturnsEmptyWidgets(t *testing.T) {
	dta := newDashboardTestApp(t)
	// No row inserted at all for this user/page.

	resp := dta.get(t, "dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeBody(resp)
	widgets, ok := body["widgets"].([]interface{})
	if !ok {
		t.Fatalf("expected widgets to be an array, got %T: %v", body["widgets"], body["widgets"])
	}
	if len(widgets) != 0 {
		t.Errorf("expected empty widgets array, got %v", widgets)
	}

	// No row should have been written as a side effect of a plain read-through.
	if _, ok := readWidgetsRow(t, dta.userID, "dashboard"); ok {
		t.Error("expected no widgets row to be created for an unseeded user/page")
	}
}

func TestGetDashboardWidgets_MalformedJSONDoesNotCrash(t *testing.T) {
	dta := newDashboardTestApp(t)
	insertWidgetsRow(t, dta.userID, "dashboard", `{not valid json`)

	resp := dta.get(t, "dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (fail-safe passthrough), got %d", resp.StatusCode)
	}

	// pruneStaleGatewayRefsFromWidgetsJSON returns malformed input unchanged
	// (changed=false), so getDashboardWidgets embeds it as-is in the response
	// body — the overall response is therefore itself malformed JSON at that
	// point. We only assert the handler didn't 500 and the DB row survives
	// untouched (fails safe, never crashes/loses data).
	got, ok := readWidgetsRow(t, dta.userID, "dashboard")
	if !ok {
		t.Fatal("expected widgets row to still exist")
	}
	if got != `{not valid json` {
		t.Errorf("malformed DB row should be left untouched, got %q", got)
	}
}

func TestGetDashboardWidgets_NoAuth_Returns401(t *testing.T) {
	dta := newDashboardTestApp(t)

	ta := &testApp{app: dta.app}
	resp := ta.do("GET", "/api/dashboard/widgets?page=dashboard", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no auth: expected 401, got %d", resp.StatusCode)
	}
}

// mustMarshal re-serializes the decoded "widgets" field back to a JSON string
// so tests can assert on substring presence/absence of graph keys.
func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
