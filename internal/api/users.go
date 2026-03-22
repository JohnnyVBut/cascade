// users.go — HTTP handlers for multi-user management and TOTP (2FA) setup.
//
// All routes require AuthMiddleware (registered after the auth gate in main.go).
//
// Routes:
//
//	GET    /api/users                    — list all users
//	POST   /api/users                    — create user
//	GET    /api/users/me                 — current user info
//	PATCH  /api/users/me                 — update own password
//	PATCH  /api/users/:id                — update username/password
//	DELETE /api/users/:id                — delete user
//
//	GET    /api/users/me/totp/setup      — generate TOTP secret + QR
//	POST   /api/users/me/totp/enable     — confirm and activate TOTP
//	POST   /api/users/me/totp/disable    — deactivate TOTP (requires code)
package api

import (
	"encoding/base64"

	"github.com/gofiber/fiber/v2"
	totpLib "github.com/pquerna/otp/totp"
	"rsc.io/qr"

	"github.com/JohnnyVBut/cascade/internal/users"
)

// issuerName is displayed in authenticator apps (e.g. "WireSteer (admin)").
const issuerName = "WireSteer"

// RegisterUsers registers all /api/users/* routes on the given router group.
// All routes require the caller to be authenticated (AuthMiddleware applied upstream).
func RegisterUsers(api fiber.Router) {
	// GET /api/users — list all users.
	api.Get("/users", listUsers)

	// POST /api/users — create a new user.
	api.Post("/users", createUser)

	// GET /api/users/me — return current user info.
	// Must be registered before /users/:id to avoid "me" being treated as an ID.
	api.Get("/users/me", getMe)

	// PATCH /api/users/me — update own password.
	api.Patch("/users/me", updateMe)

	// TOTP management for the current user.
	api.Get("/users/me/totp/setup", totpSetup)
	api.Post("/users/me/totp/enable", totpEnable)
	api.Post("/users/me/totp/disable", totpDisable)

	// PATCH /api/users/:id — update username or password.
	api.Patch("/users/:id", updateUser)

	// DELETE /api/users/:id — delete user.
	api.Delete("/users/:id", deleteUser)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// GET /api/users
func listUsers(c *fiber.Ctx) error {
	all, err := users.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"users": all})
}

// POST /api/users
// Body: { "username": "...", "password": "..." }
func createUser(c *fiber.Ctx) error {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	u, err := users.Create(body.Username, body.Password)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"user": u})
}

// GET /api/users/me
func getMe(c *fiber.Ctx) error {
	userID, ok := currentUserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "not authenticated")
	}
	u, err := users.GetByID(userID)
	if err != nil || u == nil {
		return fiber.NewError(fiber.StatusNotFound, "user not found")
	}
	return c.JSON(u)
}

// PATCH /api/users/me
// Body: { "password": "..." }
func updateMe(c *fiber.Ctx) error {
	userID, ok := currentUserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "not authenticated")
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	if body.Password != "" {
		if err := users.UpdatePassword(userID, body.Password); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}

	u, _ := users.GetByID(userID)
	return c.JSON(fiber.Map{"user": u})
}

// PATCH /api/users/:id
// Body: { "username": "...", "password": "..." } (both optional)
func updateUser(c *fiber.Ctx) error {
	id := c.Params("id")
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	if body.Username != "" {
		if err := users.UpdateUsername(id, body.Username); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}
	if body.Password != "" {
		if err := users.UpdatePassword(id, body.Password); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}

	u, _ := users.GetByID(id)
	return c.JSON(fiber.Map{"user": u})
}

// DELETE /api/users/:id
func deleteUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if err := users.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

// ── TOTP handlers ─────────────────────────────────────────────────────────────

// GET /api/users/me/totp/setup
// Generates a new TOTP key, stores the secret in the session, and returns:
//
//	{ "secret": "BASE32SECRET", "qr_uri": "otpauth://...", "qr_png": "data:image/png;base64,..." }
func totpSetup(c *fiber.Ctx) error {
	userID, ok := currentUserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "not authenticated")
	}

	u, err := users.GetByID(userID)
	if err != nil || u == nil {
		return fiber.NewError(fiber.StatusNotFound, "user not found")
	}

	// Generate a new TOTP key for the user.
	key, err := totpLib.Generate(totpLib.GenerateOpts{
		Issuer:      issuerName,
		AccountName: u.Username,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to generate TOTP key")
	}

	// Store the pending secret in the session so /enable can verify & save it.
	store := getAuthStore()
	if store != nil {
		sess, err := store.Get(c)
		if err == nil {
			sess.Set(sessKeyTOTPSetup, key.Secret())
			_ = sess.Save()
		}
	}

	// Generate QR PNG using rsc.io/qr and base64-encode it.
	qrPNG, err := generateQRPNG(key.URL())
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to generate QR code")
	}

	return c.JSON(fiber.Map{
		"secret": key.Secret(),
		"qr_uri": key.URL(),
		"qr_png": "data:image/png;base64," + qrPNG,
	})
}

// POST /api/users/me/totp/enable
// Body: { "code": "123456" }
// Verifies the TOTP code against the pending secret stored in the session,
// then saves the secret and enables TOTP for the current user.
func totpEnable(c *fiber.Ctx) error {
	userID, ok := currentUserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "not authenticated")
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	// Retrieve the pending secret from the session.
	store := getAuthStore()
	if store == nil {
		return fiber.NewError(fiber.StatusInternalServerError, "auth not initialised")
	}

	sess, err := store.Get(c)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "no TOTP setup in progress")
	}

	secret, ok2 := sess.Get(sessKeyTOTPSetup).(string)
	if !ok2 || secret == "" {
		return fiber.NewError(fiber.StatusBadRequest, "no TOTP setup in progress — call /setup first")
	}

	// Validate the code against the pending secret.
	if !totpLib.Validate(body.Code, secret) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid TOTP code",
		})
	}

	// Persist the secret and mark TOTP enabled.
	if err := users.SetTOTP(userID, secret); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	// Clear the pending secret from the session.
	sess.Delete(sessKeyTOTPSetup)
	_ = sess.Save()

	u, _ := users.GetByID(userID)
	return c.JSON(fiber.Map{"user": u})
}

// POST /api/users/me/totp/disable
// Body: { "code": "123456" }
// Verifies the current TOTP code then clears the secret and disables TOTP.
func totpDisable(c *fiber.Ctx) error {
	userID, ok := currentUserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "not authenticated")
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	secret, err := users.GetTOTPSecret(userID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if secret == "" {
		return fiber.NewError(fiber.StatusBadRequest, "TOTP is not enabled")
	}

	if !totpLib.Validate(body.Code, secret) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid TOTP code",
		})
	}

	if err := users.ClearTOTP(userID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	u, _ := users.GetByID(userID)
	return c.JSON(fiber.Map{"user": u})
}

// ── QR helpers ────────────────────────────────────────────────────────────────

// generateQRPNG encodes content as a QR PNG and returns it base64-encoded.
// Uses rsc.io/qr — the Code.PNG() method returns a PNG image byte slice.
func generateQRPNG(content string) (string, error) {
	code, err := qr.Encode(content, qr.M)
	if err != nil {
		return "", err
	}
	pngBytes := code.PNG()
	return base64.StdEncoding.EncodeToString(pngBytes), nil
}
