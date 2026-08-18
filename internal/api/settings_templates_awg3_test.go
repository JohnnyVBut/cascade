// Tests for the /api/templates/generate and /api/templates/:id/apply HTTP
// handlers' AWG 3.0 version-scoping behaviour added in the AWG3-protocol
// redo (items 8-9 of the test plan).
//
// Each test opens its own temporary SQLite DB via db.Init()/db.Close(),
// following the same per-test DB lifecycle already used elsewhere in this
// package (see system_test.go) — NOT the package-level TestMain DB used by
// import_client_configs_test.go, which is reserved for tunnel.Manager
// singleton-dependent tests.
package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/settings"
)

func initTemplatesTestApp(t *testing.T) *fiber.App {
	t.Helper()
	dir, err := os.MkdirTemp("", "cascade-api-templates-test-*")
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

	app := fiber.New()
	apiGroup := app.Group("/api")
	RegisterSettings(apiGroup)
	return app
}

func doJSON(t *testing.T, app *fiber.App, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	rr := httptest.NewRecorder()
	rr.Code = resp.StatusCode
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return rr, out
}

// ── POST /api/templates/generate ─────────────────────────────────────────────

func TestGenerateTemplate_WithAWG3Toggle_SavesAsVersion3_0(t *testing.T) {
	app := initTemplatesTestApp(t)

	rr, out := doJSON(t, app, "POST", "/api/templates/generate", map[string]any{
		"saveName":        "Gen-V3",
		"randomizeTimers": true,
	})
	if rr.Code != 200 {
		t.Fatalf("status = %d, body = %+v", rr.Code, out)
	}
	tmplRaw, ok := out["template"].(map[string]any)
	if !ok {
		t.Fatalf("response missing 'template' object: %+v", out)
	}
	if tmplRaw["version"] != "3.0" {
		t.Errorf("template.version = %v, want '3.0'", tmplRaw["version"])
	}

	// Re-fetch via GET to confirm persistence, not just the response echo.
	id, _ := tmplRaw["id"].(string)
	if id == "" {
		t.Fatal("template.id missing from response")
	}
	got, err := settings.GetTemplate(id)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got == nil {
		t.Fatal("template not found after generate+save")
	}
	if got.Version != "3.0" {
		t.Errorf("persisted Version = %q, want '3.0'", got.Version)
	}
}

func TestGenerateTemplate_HeaderProtectionToggle_SavesAsVersion3_0(t *testing.T) {
	app := initTemplatesTestApp(t)

	rr, out := doJSON(t, app, "POST", "/api/templates/generate", map[string]any{
		"saveName":         "Gen-V3-HP",
		"headerProtection": true,
	})
	if rr.Code != 200 {
		t.Fatalf("status = %d, body = %+v", rr.Code, out)
	}
	tmplRaw := out["template"].(map[string]any)
	if tmplRaw["version"] != "3.0" {
		t.Errorf("template.version = %v, want '3.0'", tmplRaw["version"])
	}
}

func TestGenerateTemplate_ContentPaddingToggle_SavesAsVersion3_0(t *testing.T) {
	app := initTemplatesTestApp(t)

	rr, out := doJSON(t, app, "POST", "/api/templates/generate", map[string]any{
		"saveName":       "Gen-V3-CP",
		"contentPadding": true,
	})
	if rr.Code != 200 {
		t.Fatalf("status = %d, body = %+v", rr.Code, out)
	}
	tmplRaw := out["template"].(map[string]any)
	if tmplRaw["version"] != "3.0" {
		t.Errorf("template.version = %v, want '3.0'", tmplRaw["version"])
	}
}

func TestGenerateTemplate_NoToggles_SavesAsVersion2_0(t *testing.T) {
	app := initTemplatesTestApp(t)

	rr, out := doJSON(t, app, "POST", "/api/templates/generate", map[string]any{
		"saveName": "Gen-V2",
	})
	if rr.Code != 200 {
		t.Fatalf("status = %d, body = %+v", rr.Code, out)
	}
	tmplRaw := out["template"].(map[string]any)
	if tmplRaw["version"] != "2.0" {
		t.Errorf("template.version = %v, want '2.0'", tmplRaw["version"])
	}

	id := tmplRaw["id"].(string)
	got, err := settings.GetTemplate(id)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.Version != "2.0" {
		t.Errorf("persisted Version = %q, want '2.0'", got.Version)
	}
}

// ── POST /api/templates/:id/apply?protocol= ──────────────────────────────────

func TestApplyTemplate_ProtocolMismatch_2_0Template_3_0Protocol_Rejected(t *testing.T) {
	app := initTemplatesTestApp(t)

	tmpl, err := settings.CreateTemplate(settings.Template{Name: "Only2_0", Version: "2.0"})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	rr, out := doJSON(t, app, "POST", "/api/templates/"+tmpl.ID+"/apply?protocol=amneziawg-3.0", nil)
	if rr.Code != 400 {
		t.Fatalf("status = %d, want 400; body = %+v", rr.Code, out)
	}
}

func TestApplyTemplate_ProtocolMismatch_3_0Template_2_0Protocol_Rejected(t *testing.T) {
	app := initTemplatesTestApp(t)

	tmpl, err := settings.CreateTemplate(settings.Template{
		Name: "Only3_0", Version: "3.0", S3: 12, S4: 12,
		HeaderProtectionKey: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	rr, out := doJSON(t, app, "POST", "/api/templates/"+tmpl.ID+"/apply?protocol=amneziawg-2.0", nil)
	if rr.Code != 400 {
		t.Fatalf("status = %d, want 400; body = %+v", rr.Code, out)
	}
}

func TestApplyTemplate_ProtocolMatch_Accepted(t *testing.T) {
	app := initTemplatesTestApp(t)

	tmpl, err := settings.CreateTemplate(settings.Template{Name: "MatchMe2_0", Version: "2.0", Jc: 9})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	rr, out := doJSON(t, app, "POST", "/api/templates/"+tmpl.ID+"/apply?protocol=amneziawg-2.0", nil)
	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %+v", rr.Code, out)
	}
	settingsRaw, ok := out["settings"].(map[string]any)
	if !ok {
		t.Fatalf("response missing 'settings' object: %+v", out)
	}
	if settingsRaw["jc"] != float64(9) {
		t.Errorf("settings.jc = %v, want 9", settingsRaw["jc"])
	}
}

// TestApplyTemplate_NoProtocolParam_BackwardCompatible confirms omitting the
// ?protocol= query param entirely still works (no rejection) — the param is
// optional, not required, for backward compatibility with existing callers.
func TestApplyTemplate_NoProtocolParam_BackwardCompatible(t *testing.T) {
	app := initTemplatesTestApp(t)

	// A "3.0" template applied with NO ?protocol= param at all must succeed,
	// even though "3.0" doesn't match the implicit default ("2.0") used when
	// the param IS given — proving the param is opt-in, not silently defaulted.
	tmpl, err := settings.CreateTemplate(settings.Template{
		Name: "NoParam3_0", Version: "3.0", S3: 12, S4: 12,
		HeaderProtectionKey: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	rr, out := doJSON(t, app, "POST", "/api/templates/"+tmpl.ID+"/apply", nil)
	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200 (no ?protocol= param should not reject); body = %+v", rr.Code, out)
	}
}
