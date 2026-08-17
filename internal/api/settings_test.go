// Package api — HTTP-level tests for PUT /api/settings.
//
// Tests cover explicit validation added in the quick-create feature:
//   - portPool: invalid values return 400 with a message; valid values return 200
//   - subnetPool: host-bits-set and non-CIDR values return 400; valid CIDR returns 200
//   - Other fields (dns, routerName, …) pass through without 400
//
// These complement the unit tests in internal/settings/settings_test.go which
// cover ParsePortPool and isValidSettingValue at the model layer.
package api

import (
	"net/http"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/tokens"
	"github.com/JohnnyVBut/cascade/internal/users"
)

// ── Harness ───────────────────────────────────────────────────────────────────

// settingsTestApp is a minimal Fiber application with the settings routes
// registered behind AuthMiddleware.  A single "owner" user is pre-created
// with a raw API token that can be passed to ta.do().
type settingsTestApp struct {
	app   *fiber.App
	token string // raw Bearer token for the owner user
}

func newSettingsTestApp(t *testing.T) *settingsTestApp {
	t.Helper()

	dir, err := os.MkdirTemp("", "cascade-settings-api-test-*")
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

	InitAuth("") // initialise session store

	owner, err := users.Create("owner", "ownerpass1")
	if err != nil {
		t.Fatalf("Create owner: %v", err)
	}
	_, rawToken, err := tokens.Create(owner.ID, "settings-test-token")
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
	RegisterSettings(api)

	return &settingsTestApp{app: app, token: rawToken}
}

// put is a convenience wrapper: PUT /api/settings with JSON body.
func (sta *settingsTestApp) put(t *testing.T, body any) *http.Response {
	t.Helper()
	ta := &testApp{app: sta.app, adminToken: sta.token}
	return ta.do("PUT", "/api/settings", sta.token, body)
}

// ── portPool validation ───────────────────────────────────────────────────────

func TestPutSettings_PortPool_Valid_Returns200(t *testing.T) {
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"portPool": "51831-51840"})
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("valid portPool: expected 200, got %d; body=%v", resp.StatusCode, body)
	}
	body := decodeBody(resp)
	if body["portPool"] != "51831-51840" {
		t.Errorf("portPool in response = %v, want '51831-51840'", body["portPool"])
	}
}

func TestPutSettings_PortPool_PrivilegedPort_Returns200(t *testing.T) {
	// Ports 1–1023 must be accepted (Docker/root context).
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"portPool": "433-442"})
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("privileged portPool 433-442: expected 200, got %d; body=%v", resp.StatusCode, body)
	}
}

func TestPutSettings_PortPool_Port0_Returns400(t *testing.T) {
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"portPool": "0"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("portPool=0: expected 400, got %d", resp.StatusCode)
	}
	body := decodeBody(resp)
	errMsg, _ := body["error"].(string)
	if errMsg == "" {
		t.Error("expected non-empty error message in body")
	}
}

func TestPutSettings_PortPool_NotAPort_Returns400(t *testing.T) {
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"portPool": "not-a-port"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("portPool=not-a-port: expected 400, got %d", resp.StatusCode)
	}
}

func TestPutSettings_PortPool_AboveMax_Returns400(t *testing.T) {
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"portPool": "70000"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("portPool=70000: expected 400, got %d", resp.StatusCode)
	}
}

func TestPutSettings_PortPool_StartGreaterThanEnd_Returns400(t *testing.T) {
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"portPool": "51840-51831"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("portPool start>end: expected 400, got %d", resp.StatusCode)
	}
}

// ── subnetPool validation ─────────────────────────────────────────────────────

func TestPutSettings_SubnetPool_Valid_Returns200(t *testing.T) {
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"subnetPool": "10.0.0.0/8"})
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("valid subnetPool: expected 200, got %d; body=%v", resp.StatusCode, body)
	}
	body := decodeBody(resp)
	if body["subnetPool"] != "10.0.0.0/8" {
		t.Errorf("subnetPool in response = %v, want '10.0.0.0/8'", body["subnetPool"])
	}
}

func TestPutSettings_SubnetPool_NotACIDR_Returns400(t *testing.T) {
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"subnetPool": "not-a-cidr"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("subnetPool=not-a-cidr: expected 400, got %d", resp.StatusCode)
	}
}

func TestPutSettings_SubnetPool_HostBitsSet_Returns400(t *testing.T) {
	// 192.168.1.5/16 has host bits set — must be rejected.
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"subnetPool": "192.168.1.5/16"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("subnetPool with host bits: expected 400, got %d", resp.StatusCode)
	}
	body := decodeBody(resp)
	errMsg, _ := body["error"].(string)
	if errMsg == "" {
		t.Error("expected non-empty error message for host-bits-set subnet")
	}
}

// ── defaultFwPolicy validation ────────────────────────────────────────────────

func TestPutSettings_DefaultFwPolicy_Drop_Returns200(t *testing.T) {
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"defaultFwPolicy": "drop"})
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("defaultFwPolicy=drop: expected 200, got %d; body=%v", resp.StatusCode, body)
	}
	body := decodeBody(resp)
	if body["defaultFwPolicy"] != "drop" {
		t.Errorf("defaultFwPolicy in response = %v, want 'drop'", body["defaultFwPolicy"])
	}
}

func TestPutSettings_DefaultFwPolicy_Accept_Returns200(t *testing.T) {
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"defaultFwPolicy": "accept"})
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("defaultFwPolicy=accept: expected 200, got %d; body=%v", resp.StatusCode, body)
	}
}

func TestPutSettings_DefaultFwPolicy_Invalid_Returns400(t *testing.T) {
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{"defaultFwPolicy": "reject"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("defaultFwPolicy=reject: expected 400, got %d", resp.StatusCode)
	}
	body := decodeBody(resp)
	errMsg, _ := body["error"].(string)
	if errMsg == "" {
		t.Error("expected non-empty error message for invalid defaultFwPolicy")
	}
}

// ── Other fields pass through without 400 ────────────────────────────────────

func TestPutSettings_OtherFields_Returns200(t *testing.T) {
	sta := newSettingsTestApp(t)

	resp := sta.put(t, map[string]any{
		"dns":                        "8.8.8.8",
		"defaultPersistentKeepalive": 30,
		"routerName":                 "Test-Router",
	})
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("other fields: expected 200, got %d; body=%v", resp.StatusCode, body)
	}
}

// ── 401 without token ─────────────────────────────────────────────────────────

func TestPutSettings_NoAuth_Returns401(t *testing.T) {
	sta := newSettingsTestApp(t)

	ta := &testApp{app: sta.app}
	resp := ta.do("PUT", "/api/settings", "", map[string]any{"dns": "1.1.1.1"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no auth: expected 401, got %d", resp.StatusCode)
	}
}

// ── POST /api/templates/generate — AWG 3.0 Transport Protection toggles ───────

func TestGenerateTemplate_HeaderProtection_True_PopulatesKey(t *testing.T) {
	sta := newSettingsTestApp(t)
	ta := &testApp{app: sta.app, adminToken: sta.token}

	resp := ta.do("POST", "/api/templates/generate", sta.token, map[string]any{
		"headerProtection": true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%v", resp.StatusCode, decodeBody(resp))
	}
	body := decodeBody(resp)
	params, ok := body["params"].(map[string]any)
	if !ok {
		t.Fatalf("expected params object in response, got %v", body)
	}
	hpk, _ := params["headerProtectionKey"].(string)
	if hpk == "" {
		t.Error("headerProtectionKey should be non-empty when headerProtection=true")
	}
}

func TestGenerateTemplate_HeaderProtection_False_KeyOmitted(t *testing.T) {
	sta := newSettingsTestApp(t)
	ta := &testApp{app: sta.app, adminToken: sta.token}

	resp := ta.do("POST", "/api/templates/generate", sta.token, map[string]any{
		"headerProtection": false,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%v", resp.StatusCode, decodeBody(resp))
	}
	body := decodeBody(resp)
	params, ok := body["params"].(map[string]any)
	if !ok {
		t.Fatalf("expected params object in response, got %v", body)
	}
	// omitempty on Params.HeaderProtectionKey means the key must be entirely
	// absent from the JSON, not present with an empty string.
	if _, present := params["headerProtectionKey"]; present {
		t.Errorf("headerProtectionKey key should be absent when headerProtection=false, got %v", params["headerProtectionKey"])
	}
}

func TestGenerateTemplate_ContentPadding_True_PopulatesField(t *testing.T) {
	sta := newSettingsTestApp(t)
	ta := &testApp{app: sta.app, adminToken: sta.token}

	resp := ta.do("POST", "/api/templates/generate", sta.token, map[string]any{
		"contentPadding": true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%v", resp.StatusCode, decodeBody(resp))
	}
	params := decodeBody(resp)["params"].(map[string]any)
	if v, _ := params["contentPaddingAddition"].(string); v == "" {
		t.Error("contentPaddingAddition should be non-empty when contentPadding=true")
	}
}

func TestGenerateTemplate_ContentPadding_False_FieldOmitted(t *testing.T) {
	sta := newSettingsTestApp(t)
	ta := &testApp{app: sta.app, adminToken: sta.token}

	resp := ta.do("POST", "/api/templates/generate", sta.token, map[string]any{
		"contentPadding": false,
	})
	params := decodeBody(resp)["params"].(map[string]any)
	if _, present := params["contentPaddingAddition"]; present {
		t.Errorf("contentPaddingAddition should be absent when contentPadding=false, got %v", params["contentPaddingAddition"])
	}
}

func TestGenerateTemplate_RandomizeTimers_True_PopulatesFields(t *testing.T) {
	sta := newSettingsTestApp(t)
	ta := &testApp{app: sta.app, adminToken: sta.token}

	resp := ta.do("POST", "/api/templates/generate", sta.token, map[string]any{
		"randomizeTimers": true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%v", resp.StatusCode, decodeBody(resp))
	}
	params := decodeBody(resp)["params"].(map[string]any)
	for _, key := range []string{"rekeyAfterTime", "rekeyTimeout", "rejectAfterTime", "keepaliveTimeout", "maxHandshakeAttempts"} {
		v, _ := params[key].(string)
		if v == "" {
			t.Errorf("%s should be non-empty when randomizeTimers=true", key)
		}
	}
}

func TestGenerateTemplate_RandomizeTimers_False_FieldsOmitted(t *testing.T) {
	sta := newSettingsTestApp(t)
	ta := &testApp{app: sta.app, adminToken: sta.token}

	resp := ta.do("POST", "/api/templates/generate", sta.token, map[string]any{
		"randomizeTimers": false,
	})
	params := decodeBody(resp)["params"].(map[string]any)
	for _, key := range []string{"rekeyAfterTime", "rekeyTimeout", "rejectAfterTime", "keepaliveTimeout", "maxHandshakeAttempts"} {
		if _, present := params[key]; present {
			t.Errorf("%s should be absent when randomizeTimers=false, got %v", key, params[key])
		}
	}
}

func TestGenerateTemplate_SaveName_HeaderProtection_PersistsInResponseAndRefetch(t *testing.T) {
	sta := newSettingsTestApp(t)
	ta := &testApp{app: sta.app, adminToken: sta.token}

	resp := ta.do("POST", "/api/templates/generate", sta.token, map[string]any{
		"headerProtection": true,
		"saveName":         "AWG3-Saved",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%v", resp.StatusCode, decodeBody(resp))
	}
	body := decodeBody(resp)
	tmpl, ok := body["template"].(map[string]any)
	if !ok {
		t.Fatalf("expected template object in response, got %v", body)
	}
	hpk, _ := tmpl["headerProtectionKey"].(string)
	if hpk == "" {
		t.Fatal("saved template should have non-empty headerProtectionKey in response")
	}
	id, _ := tmpl["id"].(string)
	if id == "" {
		t.Fatal("saved template response missing id")
	}

	// Re-fetch via GET /api/templates/:id and confirm the field persisted.
	getResp := ta.do("GET", "/api/templates/"+id, sta.token, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET template: expected 200, got %d", getResp.StatusCode)
	}
	getBody := decodeBody(getResp)
	refetched, ok := getBody["template"].(map[string]any)
	if !ok {
		t.Fatalf("expected template object in GET response, got %v", getBody)
	}
	refetchedKey, _ := refetched["headerProtectionKey"].(string)
	if refetchedKey != hpk {
		t.Errorf("re-fetched headerProtectionKey = %q, want %q (from generate response)", refetchedKey, hpk)
	}
}
