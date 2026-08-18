// Tests for requireAWG3ProtocolForFields and its wiring into createInterface
// (POST /api/tunnel-interfaces) and updateInterface (PATCH
// /api/tunnel-interfaces/:id) — items 10-11 of the AWG3-protocol-redo test
// plan. Before this session's fix, createInterface had NO validation at all
// for AWG 3.0 Transport Protection fields on a non-3.0 protocol (the gap
// this file targets specifically).
//
// HTTP-level tests reuse the tunnel.Manager singleton initialised by
// TestMain in import_client_configs_test.go (same package, same test
// binary) — see that file's comment on why interfaces must be seeded before
// tunnel.Init() runs. The reject-path assertions below only exercise
// requireAWG3ProtocolForFields, which runs BEFORE any call into the
// Manager/DB, so they are safe regardless of the shared singleton's state
// or which other test in this package ran immediately before.
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/JohnnyVBut/cascade/internal/awgparams"
	"github.com/JohnnyVBut/cascade/internal/peer"
)

// ── requireAWG3ProtocolForFields (direct unit tests) ─────────────────────────

func TestRequireAWG3ProtocolForFields_RejectsOnNon3_0Protocols(t *testing.T) {
	protocols := []string{"amneziawg-2.0", "wireguard-1.0", "", "amneziawg-1.0"}
	for _, proto := range protocols {
		t.Run(proto, func(t *testing.T) {
			a := &peer.AWG2Settings{HeaderProtectionKey: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}
			if err := requireAWG3ProtocolForFields(proto, a); err == nil {
				t.Errorf("expected error for protocol %q with AWG3 field set, got nil", proto)
			}
		})
	}
}

func TestRequireAWG3ProtocolForFields_AcceptsOn3_0(t *testing.T) {
	a := &peer.AWG2Settings{HeaderProtectionKey: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}
	if err := requireAWG3ProtocolForFields(awgparams.ProtocolAmneziaWG3, a); err != nil {
		t.Errorf("unexpected error for protocol amneziawg-3.0: %v", err)
	}
}

func TestRequireAWG3ProtocolForFields_NoFieldsSet_AlwaysAccepted(t *testing.T) {
	a := &peer.AWG2Settings{Jc: 5}
	for _, proto := range []string{"amneziawg-2.0", "wireguard-1.0", "amneziawg-3.0", ""} {
		if err := requireAWG3ProtocolForFields(proto, a); err != nil {
			t.Errorf("protocol %q with no AWG3 fields set: unexpected error: %v", proto, err)
		}
	}
}

// TestRequireAWG3ProtocolForFields_EachFieldIndividually confirms every one
// of the 7 AWG3 fields (not just HeaderProtectionKey) triggers the rejection
// on a non-3.0 protocol.
func TestRequireAWG3ProtocolForFields_EachFieldIndividually(t *testing.T) {
	cases := []struct {
		name  string
		build func() *peer.AWG2Settings
	}{
		{"HeaderProtectionKey", func() *peer.AWG2Settings { return &peer.AWG2Settings{HeaderProtectionKey: "x"} }},
		{"ContentPaddingAddition", func() *peer.AWG2Settings { return &peer.AWG2Settings{ContentPaddingAddition: "0-1"} }},
		{"RekeyAfterTime", func() *peer.AWG2Settings { return &peer.AWG2Settings{RekeyAfterTime: "1h"} }},
		{"RekeyTimeout", func() *peer.AWG2Settings { return &peer.AWG2Settings{RekeyTimeout: "5"} }},
		{"RejectAfterTime", func() *peer.AWG2Settings { return &peer.AWG2Settings{RejectAfterTime: "3h"} }},
		{"KeepaliveTimeout", func() *peer.AWG2Settings { return &peer.AWG2Settings{KeepaliveTimeout: "15"} }},
		{"MaxHandshakeAttempts", func() *peer.AWG2Settings { return &peer.AWG2Settings{MaxHandshakeAttempts: "20"} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := requireAWG3ProtocolForFields("amneziawg-2.0", c.build()); err == nil {
				t.Errorf("%s: expected error on protocol amneziawg-2.0, got nil", c.name)
			}
		})
	}
}

// ── HTTP: createInterface (POST /api/tunnel-interfaces) ──────────────────────

// newInterfacesTestApp builds a fiber app with the recover middleware —
// matching the production app (cmd/awg-easy/main.go) — because
// mgr().CreateInterface can reach db.DB() (which panics if the shared
// package-level DB singleton has been closed by another test's cleanup in
// this same test binary); without recover, that panic would crash the
// whole `go test` process instead of failing just this one test.
func newInterfacesTestApp() *fiber.App {
	app := fiber.New()
	app.Use(recover.New())
	apiGroup := app.Group("/api")
	RegisterInterfaces(apiGroup)
	return app
}

func TestCreateInterface_2_0Protocol_WithHeaderProtectionKey_Rejected(t *testing.T) {
	app := newInterfacesTestApp()

	body := map[string]any{
		"name":     "gate-test-2-0",
		"protocol": "amneziawg-2.0",
		"settings": map[string]any{
			"jc":                  5,
			"s3":                  12,
			"s4":                  12,
			"headerProtectionKey": "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/tunnel-interfaces", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	msg := string(bodyBytes)
	if !strings.Contains(msg, "AWG 3.0 Transport Protection") {
		t.Errorf("error message = %q, want it to mention 'AWG 3.0 Transport Protection'", msg)
	}
}

// TestCreateInterface_3_0Protocol_WithHeaderProtectionKey_NotRejectedByProtocolGate
// confirms the same request with protocol=amneziawg-3.0 passes the
// requireAWG3ProtocolForFields gate (the specific check under test). The
// request may still fail further down the pipeline in this sandbox (no
// "awg" binary available to generate keys) — that unrelated failure is not
// what this test asserts against.
func TestCreateInterface_3_0Protocol_WithHeaderProtectionKey_NotRejectedByProtocolGate(t *testing.T) {
	app := newInterfacesTestApp()

	body := map[string]any{
		"name":     "gate-test-3-0",
		"protocol": "amneziawg-3.0",
		"settings": map[string]any{
			"jc":                  5,
			"s3":                  12,
			"s4":                  12,
			"headerProtectionKey": "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/tunnel-interfaces", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	msg := string(bodyBytes)
	if strings.Contains(msg, "AWG 3.0 Transport Protection fields require protocol") {
		t.Errorf("protocol=amneziawg-3.0 request was rejected by the protocol gate: %q", msg)
	}
}

// ── HTTP: updateInterface (PATCH /api/tunnel-interfaces/:id) ─────────────────

// TestUpdateInterface_ExistingNonAWG3Interface_HeaderProtectionKeyRejected
// PATCHes the wg-import-test interface (protocol "wireguard-1.0", seeded by
// TestMain in import_client_configs_test.go) with an AWG3 field in
// `settings` — must be rejected because the interface's current protocol
// isn't "amneziawg-3.0".
func TestUpdateInterface_ExistingNonAWG3Interface_HeaderProtectionKeyRejected(t *testing.T) {
	app := newInterfacesTestApp()

	body := map[string]any{
		"settings": map[string]any{
			"s3":                  12,
			"s4":                  12,
			"headerProtectionKey": "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/api/tunnel-interfaces/"+wgIfaceID, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	msg := string(bodyBytes)
	if !strings.Contains(msg, "AWG 3.0 Transport Protection") {
		t.Errorf("error message = %q, want it to mention 'AWG 3.0 Transport Protection'", msg)
	}
}

// TestUpdateInterface_UnknownInterface_SkipsProtocolCheck confirms that a
// PATCH to a non-existent interface ID does not panic (mgr().GetInterface
// returns nil) and instead fails later with "not found" — regression check
// for the `if existing := mgr().GetInterface(id); existing != nil` guard.
func TestUpdateInterface_UnknownInterface_SkipsProtocolCheck(t *testing.T) {
	app := newInterfacesTestApp()

	body := map[string]any{
		"settings": map[string]any{
			"headerProtectionKey": "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/api/tunnel-interfaces/does-not-exist", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	// Must not be a 5xx from a nil-pointer panic recovered by Fiber's
	// default error handler behaving unexpectedly — a clean 400 "not found"
	// (from UpdateInterface) is the expected outcome.
	if resp.StatusCode >= 500 {
		t.Errorf("status = %d, want a non-5xx (e.g. 400 'not found'), got server error", resp.StatusCode)
	}
}
