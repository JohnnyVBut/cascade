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

func TestGenerateTemplate_ExplicitVersion3_SavesAsVersion3_0(t *testing.T) {
	app := initTemplatesTestApp(t)

	rr, out := doJSON(t, app, "POST", "/api/templates/generate", map[string]any{
		"saveName":        "Gen-V3",
		"version":         "3.0",
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

func TestGenerateTemplate_ExplicitVersion3WithHeaderProtection_SavesAsVersion3_0(t *testing.T) {
	app := initTemplatesTestApp(t)

	rr, out := doJSON(t, app, "POST", "/api/templates/generate", map[string]any{
		"saveName":         "Gen-V3-HP",
		"version":          "3.0",
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

func TestGenerateTemplate_ExplicitVersion3WithContentPadding_SavesAsVersion3_0(t *testing.T) {
	app := initTemplatesTestApp(t)

	rr, out := doJSON(t, app, "POST", "/api/templates/generate", map[string]any{
		"saveName":       "Gen-V3-CP",
		"version":        "3.0",
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

func TestGenerateTemplate_NoVersion_SavesAsVersion2_0(t *testing.T) {
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

// TestGenerateTemplate_TogglesWithoutExplicitVersion3_RejectedAsVersion2_0
// is a regression test: AWG 3.0 Transport Protection toggles no longer
// imply version 3.0 on their own (that inference was a source of bugs —
// a UI default of "on" for one toggle silently forced every generated
// profile into a 3.0 template). Without an explicit version:"3.0", the
// request defaults to version 2.0 and the save is rejected by
// settings.hasAWG3Fields() validation, exactly as a hand-built 2.0
// template with stray 3.0-only fields would be.
func TestGenerateTemplate_TogglesWithoutExplicitVersion3_RejectedAsVersion2_0(t *testing.T) {
	app := initTemplatesTestApp(t)

	rr, out := doJSON(t, app, "POST", "/api/templates/generate", map[string]any{
		"saveName":        "Gen-NoVersion-WithToggle",
		"randomizeTimers": true,
	})
	if rr.Code != 400 {
		t.Fatalf("status = %d, want 400; body = %+v", rr.Code, out)
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

// TestCreateTemplate_FullAWG3FieldSet_RoundTripsExactly is a regression test
// for exportTemplateJSON (internal/frontend/www/js/app.js): before this
// fix, exporting an AWG 3.0 profile and importing it via POST /api/templates
// on another server silently dropped `version` (defaulting to "2.0") and
// every AWG 3.0 Transport Protection field, because the exported JSON never
// included them. This test sends the exact full field set the fixed
// exportTemplateJSON now produces — name, version, host, jc/jmin/jmax,
// s1-s4, h1-h4, i1-i5, and all AWG3 fields — and confirms POST /api/templates
// persists every one of them exactly, and a subsequent GET returns the same
// values (not just the create-response echo).
func TestCreateTemplate_FullAWG3FieldSet_RoundTripsExactly(t *testing.T) {
	app := initTemplatesTestApp(t)

	body := map[string]any{
		"name":    "Full-AWG3-Export-Import",
		"version": "3.0",
		"host":    "www.google.com",
		"jc":      5, "jmin": 10, "jmax": 30,
		"s1": 10, "s2": 20, "s3": 12, "s4": 12,
		"h1": "1000000-2000000", "h2": "2000000-3000000",
		"h3": "3000000-4000000", "h4": "4000000-5000000",
		"i1": "<b 0x01020304>", "i2": "<b 0x05060708>",
		"i3": "", "i4": "", "i5": "",
		"headerProtectionKey":    "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		"contentPaddingAddition": "0-1",
		"rekeyAfterTime":         "1h-2h",
		"rekeyTimeout":           "5-10",
		"rejectAfterTime":        "3h",
		"keepaliveTimeout":       "15-25",
		"maxHandshakeAttempts":   "20",
		"randomTrailers":         "on",
		"disableCookies":         "on",
	}

	rr, out := doJSON(t, app, "POST", "/api/templates", body)
	if rr.Code != 201 {
		t.Fatalf("status = %d, body = %+v", rr.Code, out)
	}
	created, ok := out["template"].(map[string]any)
	if !ok {
		t.Fatalf("response missing 'template' object: %+v", out)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("template.id missing from create response")
	}

	rr2, out2 := doJSON(t, app, "GET", "/api/templates/"+id, nil)
	if rr2.Code != 200 {
		t.Fatalf("GET status = %d, body = %+v", rr2.Code, out2)
	}
	got, ok := out2["template"].(map[string]any)
	if !ok {
		t.Fatalf("GET response missing 'template' object: %+v", out2)
	}

	wantStrings := map[string]string{
		"version":                "3.0",
		"host":                   "www.google.com",
		"h1":                     "1000000-2000000",
		"h2":                     "2000000-3000000",
		"h3":                     "3000000-4000000",
		"h4":                     "4000000-5000000",
		"i1":                     "<b 0x01020304>",
		"i2":                     "<b 0x05060708>",
		"headerProtectionKey":    "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		"contentPaddingAddition": "0-1",
		"rekeyAfterTime":         "1h-2h",
		"rekeyTimeout":           "5-10",
		"rejectAfterTime":        "3h",
		"keepaliveTimeout":       "15-25",
		"maxHandshakeAttempts":   "20",
		"randomTrailers":         "on",
		"disableCookies":         "on",
	}
	for k, want := range wantStrings {
		if got[k] != want {
			t.Errorf("GET template.%s = %v, want %q", k, got[k], want)
		}
	}
	wantNumbers := map[string]float64{
		"jc": 5, "jmin": 10, "jmax": 30,
		"s1": 10, "s2": 20, "s3": 12, "s4": 12,
	}
	for k, want := range wantNumbers {
		if got[k] != want {
			t.Errorf("GET template.%s = %v, want %v", k, got[k], want)
		}
	}
}
