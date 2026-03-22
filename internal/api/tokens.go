// tokens.go — HTTP handlers for API token management.
//
// API tokens allow programmatic access without session cookies or TOTP.
// Tokens are long-lived and prefixed with "ws_" for easy identification.
// The raw token value is returned ONCE at creation; only its SHA-256 hash
// is stored in the database.
//
// Routes (all behind AuthMiddleware):
//
//	GET    /api/tokens      — list current user's tokens (id, name, last_used, created_at)
//	POST   /api/tokens      — create token → { token: {...}, raw_token: "ws_..." }
//	DELETE /api/tokens/:id  — revoke token
package api

import (
	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/awg-easy/internal/tokens"
)

// RegisterTokens registers API token management endpoints on the router group.
func RegisterTokens(api fiber.Router) {
	api.Get("/tokens", listTokens)
	api.Post("/tokens", createToken)
	api.Delete("/tokens/:id", deleteToken)
}

// GET /api/tokens — list all tokens belonging to the current user.
func listTokens(c *fiber.Ctx) error {
	userID, ok := currentUserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "not authenticated")
	}
	list, err := tokens.ListByUser(userID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"tokens": list})
}

// POST /api/tokens — create a new API token.
// Body: { "name": "..." }
// Response: { "token": { id, name, last_used, created_at }, "raw_token": "ws_..." }
// raw_token is shown ONCE — save it, it cannot be retrieved later.
func createToken(c *fiber.Ctx) error {
	userID, ok := currentUserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "not authenticated")
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	if body.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name is required")
	}

	tok, rawToken, err := tokens.Create(userID, body.Name)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"token":     tok,
		"raw_token": rawToken,
	})
}

// DELETE /api/tokens/:id — revoke (delete) a token.
// Only the token owner can delete it.
func deleteToken(c *fiber.Ctx) error {
	userID, ok := currentUserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "not authenticated")
	}

	id := c.Params("id")
	if err := tokens.Delete(id, userID); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}
