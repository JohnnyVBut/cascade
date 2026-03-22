// auth.go — session-based authentication middleware and login/logout endpoints.
//
// Supports multi-user authentication with optional TOTP (2FA).
//
// Two-step login flow:
//  1. POST /api/session  { username, password }
//     → If TOTP enabled: { totp_required: true }, session totp_pending=true
//     → If TOTP disabled: { authenticated: true }, session authenticated=true
//  2. POST /api/auth/totp/verify  { code }
//     → Validates TOTP code, upgrades session to authenticated=true
//
// Authorization header fallback (for scripts / curl):
//   "username:password" (colon-separated) → verifies against users table
//   raw password (no colon) → tries against "admin" user for backward compat
//
// Open mode: if no users exist in the DB, all requests pass through.
// Session storage: in-memory (Fiber built-in).
// Sessions do not survive container restart — this is intentional.
package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	totpLib "github.com/pquerna/otp/totp"

	"github.com/JohnnyVBut/awg-easy/internal/tokens"
	"github.com/JohnnyVBut/awg-easy/internal/users"
)

// localKeyTokenUserID is the fiber.Ctx Locals key set when a request is
// authenticated via a Bearer API token (instead of a session cookie).
const localKeyTokenUserID = "token_user_id"

// ── Session key constants ─────────────────────────────────────────────────────

const (
	// sessKeyAuthenticated is set to true once the user is fully authenticated.
	sessKeyAuthenticated = "authenticated"
	// sessKeyTOTPPending is set to true when password was verified but TOTP is still required.
	sessKeyTOTPPending = "totp_pending"
	// sessKeyUserID holds the authenticated (or pending) user's ID.
	sessKeyUserID = "user_id"
	// sessKeyTOTPSetup holds a pending TOTP secret during the setup flow.
	sessKeyTOTPSetup = "totp_setup_secret"
)

// ── Package-level auth state ──────────────────────────────────────────────────

var (
	authStore *session.Store
	// authPasswordHash is the bcrypt hash from the PASSWORD_HASH env var.
	// Used only for SeedAdminIfEmpty — kept here so InitAuth callers still work.
	authPasswordHash string
)

// rememberMaxAge is the session lifetime when "remember me" is checked.
const rememberMaxAge = 30 * 24 * time.Hour

// defaultSessionAge is the default session lifetime.
const defaultSessionAge = 24 * time.Hour

// InitAuth initialises the auth subsystem.
// passwordHash is the value of the PASSWORD_HASH env var (bcrypt hash).
// It is used to seed the admin user via users.SeedAdminIfEmpty on first run.
// Call once from main() before registering routes.
func InitAuth(passwordHash string) {
	authPasswordHash = strings.TrimSpace(passwordHash)

	authStore = session.New(session.Config{
		Expiration:     defaultSessionAge,
		KeyLookup:      "cookie:session_id",
		CookieHTTPOnly: true,
		CookieSameSite: "Strict",
	})
}

// ── Middleware ────────────────────────────────────────────────────────────────

// AuthMiddleware protects API routes.
//
// Pass-through when:
//  1. No users exist in the DB (open mode — first-run or empty table).
//  2. Session cookie contains authenticated=true.
//  3. Authorization: Bearer ws_... — valid API token (sets token_user_id in locals).
//  4. Authorization header: "username:password" or raw password (admin compat).
//
// Returns 401 JSON otherwise.
func AuthMiddleware(c *fiber.Ctx) error {
	// Open mode: if no users are registered yet, allow everything.
	n, err := users.Count()
	if err == nil && n == 0 {
		return c.Next()
	}

	// Session check.
	if authStore != nil {
		sess, err := authStore.Get(c)
		if err == nil && sess.Get(sessKeyAuthenticated) == true {
			return c.Next()
		}
	}

	// Authorization header: Bearer token or username:password fallback.
	if hdr := c.Get("Authorization"); hdr != "" {
		// Bearer API token — preferred for programmatic access.
		if strings.HasPrefix(hdr, "Bearer ") {
			rawToken := strings.TrimPrefix(hdr, "Bearer ")
			if userID, err := tokens.VerifyAndTouch(rawToken); err == nil && userID != "" {
				c.Locals(localKeyTokenUserID, userID)
				return c.Next()
			}
		}
		// Legacy fallback: "username:password" or raw password for "admin".
		if checkAuthHeader(hdr) {
			return c.Next()
		}
	}

	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"error": "Unauthorized",
	})
}

// checkAuthHeader tries to authenticate using the Authorization header value.
// Supports two formats:
//   - "username:password" — verified against the users table
//   - raw password (no colon) — tried against the "admin" user for backward compat
func checkAuthHeader(hdr string) bool {
	if idx := strings.IndexByte(hdr, ':'); idx >= 0 {
		username := hdr[:idx]
		password := hdr[idx+1:]
		u, err := users.VerifyPassword(username, password)
		return err == nil && u != nil
	}
	// No colon — treat as raw password for "admin" (backward compat).
	u, err := users.VerifyPassword("admin", hdr)
	return err == nil && u != nil
}

// ── Routes ────────────────────────────────────────────────────────────────────

// RegisterAuth registers the session and TOTP-verify endpoints on the router group.
// These routes are intentionally NOT behind AuthMiddleware.
//
//	GET    /api/session           — current session state
//	POST   /api/session           — login (step 1: username + password)
//	DELETE /api/session           — logout
//	POST   /api/auth/totp/verify  — login step 2: verify TOTP code
func RegisterAuth(api fiber.Router) {
	// GET /api/session — returns current authentication state.
	api.Get("/session", func(c *fiber.Ctx) error {
		// Check if any users exist.
		n, err := users.Count()
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "db error")
		}

		// Open mode: no users in DB.
		if n == 0 {
			return c.JSON(fiber.Map{
				"authenticated":    true,
				"requiresPassword": false,
				"totp_pending":     false,
				"username":         "",
			})
		}

		if authStore == nil {
			return c.JSON(fiber.Map{
				"authenticated":    false,
				"requiresPassword": true,
				"totp_pending":     false,
				"username":         "",
			})
		}

		sess, err := authStore.Get(c)
		if err != nil {
			return c.JSON(fiber.Map{
				"authenticated":    false,
				"requiresPassword": true,
				"totp_pending":     false,
				"username":         "",
			})
		}

		authenticated := sess.Get(sessKeyAuthenticated) == true
		totpPending := sess.Get(sessKeyTOTPPending) == true

		// Resolve username from session user_id when authenticated or pending.
		username := ""
		if userID, ok := sess.Get(sessKeyUserID).(string); ok && userID != "" {
			if u, _ := users.GetByID(userID); u != nil {
				username = u.Username
			}
		}

		return c.JSON(fiber.Map{
			"authenticated":    authenticated,
			"requiresPassword": true,
			"totp_pending":     totpPending,
			"username":         username,
		})
	})

	// POST /api/session — verify username + password (step 1).
	// Body: { "username": "...", "password": "...", "remember": true/false }
	// If username is empty, defaults to "admin" for backward compatibility.
	api.Post("/session", func(c *fiber.Ctx) error {
		n, err := users.Count()
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "db error")
		}
		if n == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "No password is configured",
			})
		}

		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Remember bool   `json:"remember"`
		}
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
		}

		// Default username to "admin" for backward compat with old clients
		// that only send password.
		if body.Username == "" {
			body.Username = "admin"
		}

		u, err := users.VerifyPassword(body.Username, body.Password)
		if err != nil || u == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid credentials",
			})
		}

		sess, err := authStore.Get(c)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "session error")
		}

		// Remember me → extend session lifetime.
		if body.Remember {
			sess.SetExpiry(rememberMaxAge)
		}

		if u.TOTPEnabled {
			// Password is correct but TOTP is still required.
			sess.Set(sessKeyTOTPPending, true)
			sess.Set(sessKeyUserID, u.ID)
			if err := sess.Save(); err != nil {
				return fiber.NewError(fiber.StatusInternalServerError, "session save error")
			}
			return c.JSON(fiber.Map{"totp_required": true})
		}

		// No TOTP — fully authenticated.
		sess.Set(sessKeyAuthenticated, true)
		sess.Set(sessKeyUserID, u.ID)
		if err := sess.Save(); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "session save error")
		}
		return c.JSON(fiber.Map{"authenticated": true})
	})

	// DELETE /api/session — destroy the session (logout).
	api.Delete("/session", func(c *fiber.Ctx) error {
		if authStore != nil {
			sess, err := authStore.Get(c)
			if err == nil {
				_ = sess.Destroy()
			}
		}
		return c.JSON(fiber.Map{"authenticated": false})
	})

	// POST /api/auth/totp/verify — step 2 of TOTP login.
	// Requires totp_pending=true in the session (set by POST /api/session).
	// Body: { "code": "123456" }
	api.Post("/auth/totp/verify", func(c *fiber.Ctx) error {
		if authStore == nil {
			return fiber.NewError(fiber.StatusInternalServerError, "auth not initialised")
		}

		sess, err := authStore.Get(c)
		if err != nil || sess.Get(sessKeyTOTPPending) != true {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "No pending TOTP session",
			})
		}

		userID, ok := sess.Get(sessKeyUserID).(string)
		if !ok || userID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "No pending TOTP session",
			})
		}

		var body struct {
			Code string `json:"code"`
		}
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
		}

		secret, err := users.GetTOTPSecret(userID)
		if err != nil || secret == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "TOTP not configured for this user",
			})
		}

		if !totpLib.Validate(body.Code, secret) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid TOTP code",
			})
		}

		// Upgrade session: clear pending flag, set authenticated.
		sess.Delete(sessKeyTOTPPending)
		sess.Set(sessKeyAuthenticated, true)
		if err := sess.Save(); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "session save error")
		}

		return c.JSON(fiber.Map{"authenticated": true})
	})
}

// ── Session helpers (used by users.go handlers) ───────────────────────────────

// currentUserID extracts the user ID from the current request context.
// Checks (in order):
//  1. Locals["token_user_id"] — set by Bearer token auth in AuthMiddleware.
//  2. Session cookie — set by POST /api/session login flow.
//
// Returns ("", false) if neither source provides a user ID.
func currentUserID(c *fiber.Ctx) (string, bool) {
	// Token-authenticated request: user ID is stored in locals.
	if id, ok := c.Locals(localKeyTokenUserID).(string); ok && id != "" {
		return id, true
	}
	// Session-authenticated request.
	if authStore == nil {
		return "", false
	}
	sess, err := authStore.Get(c)
	if err != nil {
		return "", false
	}
	id, ok := sess.Get(sessKeyUserID).(string)
	return id, ok && id != ""
}

// getAuthStore returns the package-level session store.
// Used by users.go to store the TOTP setup secret in the session.
func getAuthStore() *session.Store {
	return authStore
}
