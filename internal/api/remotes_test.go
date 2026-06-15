// Package api — HTTP-level tests for POST /api/remotes (addRemote handler).
//
// These tests cover the request-validation logic of addRemote, which runs
// BEFORE any network call to the remote server:
//   - name and url are always required
//   - url must be http/https and must not point to localhost/link-local (SSRF)
//   - login mode (no token): username and password are required
//   - token mode (token set): username/password are NOT required
//
// The network-dependent behaviour (token validation via Ping, login via
// ObtainToken) is covered separately in internal/remoteclient/client_test.go,
// because httptest servers listen on 127.0.0.1, which addRemote's SSRF guard
// deliberately rejects — so success paths cannot be exercised through the
// handler without disabling that guard.
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

// remotesTestApp is a minimal Fiber application with the remotes routes
// registered behind AuthMiddleware. A single "owner" user is pre-created
// with a raw API token usable as a Bearer credential.
type remotesTestApp struct {
	app   *fiber.App
	token string
}

func newRemotesTestApp(t *testing.T) *remotesTestApp {
	t.Helper()

	dir, err := os.MkdirTemp("", "cascade-remotes-api-test-*")
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

	InitAuth("")

	owner, err := users.Create("owner", "ownerpass1")
	if err != nil {
		t.Fatalf("Create owner: %v", err)
	}
	_, rawToken, err := tokens.Create(owner.ID, "remotes-test-token")
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
	RegisterRemotes(api)

	return &remotesTestApp{app: app, token: rawToken}
}

// post is a convenience wrapper: POST /api/remotes/ with a JSON body and the
// owner's Bearer token.
func (rta *remotesTestApp) post(t *testing.T, body any) *http.Response {
	t.Helper()
	ta := &testApp{app: rta.app, adminToken: rta.token}
	return ta.do("POST", "/api/remotes/", rta.token, body)
}

// ── Required-field validation ─────────────────────────────────────────────────

func TestAddRemote_MissingName_Returns400(t *testing.T) {
	rta := newRemotesTestApp(t)

	resp := rta.post(t, map[string]any{"url": "https://r.example.com"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing name: expected 400, got %d", resp.StatusCode)
	}
	if msg, _ := decodeBody(resp)["error"].(string); msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestAddRemote_MissingURL_Returns400(t *testing.T) {
	rta := newRemotesTestApp(t)

	resp := rta.post(t, map[string]any{"name": "Berlin"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing url: expected 400, got %d", resp.StatusCode)
	}
}

func TestAddRemote_InvalidJSON_Returns400(t *testing.T) {
	rta := newRemotesTestApp(t)

	// Send a raw non-JSON body.
	ta := &testApp{app: rta.app, adminToken: rta.token}
	resp := ta.do("POST", "/api/remotes/", rta.token, "not-json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid JSON: expected 400, got %d", resp.StatusCode)
	}
}

// ── URL validation (SSRF guard) ───────────────────────────────────────────────

func TestAddRemote_URLNoScheme_Returns400(t *testing.T) {
	rta := newRemotesTestApp(t)

	resp := rta.post(t, map[string]any{"name": "x", "url": "r.example.com", "token": "ws_abc"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("url without scheme: expected 400, got %d", resp.StatusCode)
	}
}

func TestAddRemote_URLLocalhost_Returns400(t *testing.T) {
	rta := newRemotesTestApp(t)

	resp := rta.post(t, map[string]any{"name": "x", "url": "http://localhost:8080", "token": "ws_abc"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("localhost url: expected 400, got %d", resp.StatusCode)
	}
}

func TestAddRemote_URLLoopbackIP_Returns400(t *testing.T) {
	rta := newRemotesTestApp(t)

	resp := rta.post(t, map[string]any{"name": "x", "url": "http://127.0.0.1:8080", "token": "ws_abc"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("127.0.0.1 url: expected 400, got %d", resp.StatusCode)
	}
}

// ── Login-mode credential validation ──────────────────────────────────────────

func TestAddRemote_LoginMode_MissingUsername_Returns400(t *testing.T) {
	rta := newRemotesTestApp(t)

	// 198.51.100.1 is TEST-NET-2 (RFC 5737): public (not blocked by the SSRF
	// guard) so URL validation passes and the credential check is reached, but
	// no real connection is made because the handler returns 400 before Ping.
	resp := rta.post(t, map[string]any{"name": "x", "url": "https://198.51.100.1", "password": "p"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("login mode missing username: expected 400, got %d", resp.StatusCode)
	}
}

func TestAddRemote_LoginMode_MissingPassword_Returns400(t *testing.T) {
	rta := newRemotesTestApp(t)

	resp := rta.post(t, map[string]any{"name": "x", "url": "https://198.51.100.1", "username": "u"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("login mode missing password: expected 400, got %d", resp.StatusCode)
	}
}

func TestAddRemote_LoginMode_NoCredentials_Returns400(t *testing.T) {
	rta := newRemotesTestApp(t)

	// Neither token nor username/password — login mode requires credentials.
	resp := rta.post(t, map[string]any{"name": "x", "url": "https://198.51.100.1"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("login mode no credentials: expected 400, got %d", resp.StatusCode)
	}
}

// Note: the token-mode happy path (token set → Ping validation → 201) and its
// failure modes are covered in internal/remoteclient/client_test.go, because the
// SSRF guard rejects the 127.0.0.1 address that httptest servers bind to, and a
// real unroutable address would make the handler block on the client's 15s
// timeout. The handler's branch wiring is straight-line and both halves
// (require-credentials in login mode; Ping in token mode) are tested in isolation.

// ── Auth ──────────────────────────────────────────────────────────────────────

func TestAddRemote_NoAuth_Returns401(t *testing.T) {
	rta := newRemotesTestApp(t)

	ta := &testApp{app: rta.app}
	resp := ta.do("POST", "/api/remotes/", "", map[string]any{"name": "x", "url": "https://r.example.com", "token": "ws_abc"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no auth: expected 401, got %d", resp.StatusCode)
	}
}
