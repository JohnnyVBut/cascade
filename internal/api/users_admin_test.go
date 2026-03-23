// Package api — integration tests for MED-3: Admin Role + Privilege Escalation Fix.
//
// Tests cover:
//   - listUsers (GET /api/users) — admin only
//   - createUser (POST /api/users) — admin only
//   - updateUser (PATCH /api/users/:id) — admin OR owner
//   - deleteUser (DELETE /api/users/:id) — admin OR owner
//   - setAdmin (POST /api/users/:id/set-admin) — admin only, last-admin guard
//
// Authorization is exercised via Bearer API tokens — the only mechanism that
// sets the user ID in c.Locals and therefore propagates identity to callerIsAdmin.
// (The "username:password" Authorization header path does NOT set user ID in
// context, so callerIsAdmin always returns false for that path. Bearer tokens
// are the correct path for programmatic access.)
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/tokens"
	"github.com/JohnnyVBut/cascade/internal/users"
)

// ── Test harness ──────────────────────────────────────────────────────────────

// testApp holds the Fiber application and a set of pre-created test users with tokens.
type testApp struct {
	app        *fiber.App
	admin      *users.User
	adminToken string // raw Bearer token
	alice      *users.User
	aliceToken string
	bob        *users.User
	bobToken   string
}

// buildFiberApp constructs a Fiber application with AuthMiddleware and all
// user management routes — the same configuration used in production.
func buildFiberApp() *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		// Return errors as JSON for easier assertion.
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

	api := app.Group("/api")

	// Auth endpoints (POST /api/session etc.) — not behind AuthMiddleware.
	RegisterAuth(api)

	// Protected routes.
	protected := api.Group("", AuthMiddleware)
	RegisterUsers(protected)

	return app
}

// newTestApp initialises a fresh SQLite DB, creates an admin and two regular
// users with API tokens, builds the Fiber app, and registers cleanup.
//
// Bearer tokens are used for all authenticated requests because they are the
// only auth path that sets user ID into c.Locals, which callerIsAdmin requires.
func newTestApp(t *testing.T) *testApp {
	t.Helper()

	dir, err := os.MkdirTemp("", "cascade-api-test-*")
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

	// InitAuth must be called so the session store is initialised.
	InitAuth("")

	// Create admin user.
	adminUser, err := users.Create("admin", "adminpass")
	if err != nil {
		t.Fatalf("Create admin: %v", err)
	}
	// Grant admin role. SetAdmin(true) has no restriction (only removing last admin is blocked).
	if err := users.SetAdmin(adminUser.ID, true); err != nil {
		t.Fatalf("SetAdmin(admin, true): %v", err)
	}
	// Refresh to pick up the updated IsAdmin field.
	adminUser, err = users.GetByID(adminUser.ID)
	if err != nil || adminUser == nil {
		t.Fatalf("GetByID(admin): %v", err)
	}

	// Create an API token for admin.
	_, adminRaw, err := tokens.Create(adminUser.ID, "admin-test-token")
	if err != nil {
		t.Fatalf("tokens.Create(admin): %v", err)
	}

	// Create two regular users.
	alice, err := users.Create("alice", "alicepass")
	if err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	_, aliceRaw, err := tokens.Create(alice.ID, "alice-test-token")
	if err != nil {
		t.Fatalf("tokens.Create(alice): %v", err)
	}

	bob, err := users.Create("bob", "bobpass")
	if err != nil {
		t.Fatalf("Create bob: %v", err)
	}
	_, bobRaw, err := tokens.Create(bob.ID, "bob-test-token")
	if err != nil {
		t.Fatalf("tokens.Create(bob): %v", err)
	}

	return &testApp{
		app:        buildFiberApp(),
		admin:      adminUser,
		adminToken: adminRaw,
		alice:      alice,
		aliceToken: aliceRaw,
		bob:        bob,
		bobToken:   bobRaw,
	}
}

// do sends a request to the Fiber test app and returns the response.
// bearerToken is optional; when non-empty it is set as a Bearer Authorization header.
func (ta *testApp) do(method, path, bearerToken string, body any) *http.Response {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}

	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := ta.app.Test(req, -1)
	if err != nil {
		panic(fmt.Sprintf("app.Test: %v", err))
	}
	return resp
}

// decodeBody reads and JSON-decodes the response body into a map.
func decodeBody(resp *http.Response) map[string]any {
	var m map[string]any
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &m)
	return m
}

// ── GET /api/users ────────────────────────────────────────────────────────────

func TestListUsers_AdminGets200(t *testing.T) {
	ta := newTestApp(t)

	resp := ta.do("GET", "/api/users", ta.adminToken, nil)
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("admin GET /api/users: expected 200, got %d; body=%v", resp.StatusCode, body)
	}

	body := decodeBody(resp)
	if _, ok := body["users"]; !ok {
		t.Error("response should contain 'users' key")
	}
}

func TestListUsers_NonAdminGets403(t *testing.T) {
	ta := newTestApp(t)

	resp := ta.do("GET", "/api/users", ta.aliceToken, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin GET /api/users: expected 403, got %d", resp.StatusCode)
	}
}

func TestListUsers_UnauthenticatedGets401(t *testing.T) {
	ta := newTestApp(t)

	resp := ta.do("GET", "/api/users", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET /api/users: expected 401, got %d", resp.StatusCode)
	}
}

// ── POST /api/users ───────────────────────────────────────────────────────────

func TestCreateUser_AdminGets201(t *testing.T) {
	ta := newTestApp(t)

	resp := ta.do("POST", "/api/users",
		ta.adminToken,
		map[string]string{"username": "charlie", "password": "charliepass"},
	)
	if resp.StatusCode != http.StatusCreated {
		body := decodeBody(resp)
		t.Errorf("admin POST /api/users: expected 201, got %d; body=%v", resp.StatusCode, body)
	}

	body := decodeBody(resp)
	if _, ok := body["user"]; !ok {
		t.Error("response should contain 'user' key")
	}
}

func TestCreateUser_NonAdminGets403(t *testing.T) {
	ta := newTestApp(t)

	resp := ta.do("POST", "/api/users",
		ta.aliceToken,
		map[string]string{"username": "eve", "password": "evepass"},
	)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin POST /api/users: expected 403, got %d", resp.StatusCode)
	}
}

// ── PATCH /api/users/:id ──────────────────────────────────────────────────────

func TestUpdateUser_OwnerCanUpdateSelf(t *testing.T) {
	ta := newTestApp(t)

	resp := ta.do("PATCH", "/api/users/"+ta.alice.ID,
		ta.aliceToken,
		map[string]string{"password": "newAlicePass"},
	)
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("owner PATCH own account: expected 200, got %d; body=%v", resp.StatusCode, body)
	}
}

func TestUpdateUser_NonOwnerNonAdminGets403(t *testing.T) {
	ta := newTestApp(t)

	// Alice tries to update Bob's account.
	resp := ta.do("PATCH", "/api/users/"+ta.bob.ID,
		ta.aliceToken,
		map[string]string{"password": "hackbob"},
	)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-owner non-admin PATCH other user: expected 403, got %d", resp.StatusCode)
	}
}

func TestUpdateUser_AdminCanUpdateAnyUser(t *testing.T) {
	ta := newTestApp(t)

	resp := ta.do("PATCH", "/api/users/"+ta.alice.ID,
		ta.adminToken,
		map[string]string{"password": "newPassForAlice"},
	)
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("admin PATCH any user: expected 200, got %d; body=%v", resp.StatusCode, body)
	}
}

// ── DELETE /api/users/:id ─────────────────────────────────────────────────────

func TestDeleteUser_OwnerCanDeleteSelf(t *testing.T) {
	ta := newTestApp(t)

	// Bob deletes his own account.
	resp := ta.do("DELETE", "/api/users/"+ta.bob.ID,
		ta.bobToken,
		nil,
	)
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("owner DELETE own account: expected 200, got %d; body=%v", resp.StatusCode, body)
	}
}

func TestDeleteUser_NonOwnerNonAdminGets403(t *testing.T) {
	ta := newTestApp(t)

	// Alice tries to delete Bob's account.
	resp := ta.do("DELETE", "/api/users/"+ta.bob.ID,
		ta.aliceToken,
		nil,
	)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-owner non-admin DELETE other user: expected 403, got %d", resp.StatusCode)
	}
}

func TestDeleteUser_AdminCanDeleteAnyUser(t *testing.T) {
	ta := newTestApp(t)

	resp := ta.do("DELETE", "/api/users/"+ta.alice.ID,
		ta.adminToken,
		nil,
	)
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("admin DELETE any user: expected 200, got %d; body=%v", resp.StatusCode, body)
	}
}

// ── POST /api/users/:id/set-admin ─────────────────────────────────────────────

func TestSetAdmin_AdminCanGrantAdminRole(t *testing.T) {
	ta := newTestApp(t)

	resp := ta.do("POST", "/api/users/"+ta.alice.ID+"/set-admin",
		ta.adminToken,
		map[string]bool{"admin": true},
	)
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("admin POST set-admin(true): expected 200, got %d; body=%v", resp.StatusCode, body)
	}

	// Verify Alice now has admin role via the users package.
	admin, err := users.IsAdmin(ta.alice.ID)
	if err != nil {
		t.Fatalf("IsAdmin(alice): %v", err)
	}
	if !admin {
		t.Error("expected Alice to be admin after set-admin call")
	}
}

func TestSetAdmin_NonAdminGets403(t *testing.T) {
	ta := newTestApp(t)

	resp := ta.do("POST", "/api/users/"+ta.bob.ID+"/set-admin",
		ta.aliceToken,
		map[string]bool{"admin": true},
	)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin POST set-admin: expected 403, got %d", resp.StatusCode)
	}
}

func TestSetAdmin_LastAdminCannotRemoveOwnAdminRole(t *testing.T) {
	ta := newTestApp(t)

	// Only 1 admin (admin). Attempt to demote via set-admin.
	resp := ta.do("POST", "/api/users/"+ta.admin.ID+"/set-admin",
		ta.adminToken,
		map[string]bool{"admin": false},
	)
	if resp.StatusCode != http.StatusBadRequest {
		body := decodeBody(resp)
		t.Errorf("last admin remove own role: expected 400, got %d; body=%v", resp.StatusCode, body)
	}
}

func TestSetAdmin_CanRemoveAdminWhenTwoAdminsExist(t *testing.T) {
	ta := newTestApp(t)

	// Promote Alice to admin so there are 2 admins.
	if err := users.SetAdmin(ta.alice.ID, true); err != nil {
		t.Fatalf("SetAdmin(alice, true): %v", err)
	}

	// Now admin can remove their own admin role because Alice is still admin.
	resp := ta.do("POST", "/api/users/"+ta.admin.ID+"/set-admin",
		ta.adminToken,
		map[string]bool{"admin": false},
	)
	if resp.StatusCode != http.StatusOK {
		body := decodeBody(resp)
		t.Errorf("remove admin when 2 admins exist: expected 200, got %d; body=%v", resp.StatusCode, body)
	}
}

// ── callerIsAdmin open-mode behaviour ────────────────────────────────────────

// TestListUsers_OpenModeNoAuth verifies that when there are no users in the DB
// (open mode), GET /api/users passes both authentication AND the admin check.
//
// Open mode is the first-run scenario: empty users table.
// callerIsAdmin returns true when Count() == 0, so no token is needed.
func TestListUsers_OpenModeNoAuth(t *testing.T) {
	// Build a fresh DB with NO users.
	dir, err := os.MkdirTemp("", "cascade-api-openmode-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if err := db.Init(dir); err != nil {
		db.Close()
		os.RemoveAll(dir)
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})

	InitAuth("")

	app := buildFiberApp()

	req := httptest.NewRequest("GET", "/api/users", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("open mode (no users) GET /api/users: expected 200, got %d", resp.StatusCode)
	}
}
