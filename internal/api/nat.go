// nat.go — HTTP handlers for NatManager (Outbound NAT / MASQUERADE rules).
//
// Routes:
//
//	GET    /api/nat/interfaces  ← host network interfaces (for outInterface dropdown)
//	GET    /api/nat/rules
//	POST   /api/nat/rules
//	PATCH  /api/nat/rules/:id
//	DELETE /api/nat/rules/:id
package api

import (
	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/nat"
)

// RegisterNat registers all /api/nat/* routes.
func RegisterNat(api fiber.Router) {
	g := api.Group("/nat")

	g.Get("/interfaces", getNatInterfaces)
	g.Get("/rules", getNatRules)
	g.Post("/rules", createNatRule)
	g.Patch("/rules/:id", updateNatRule)
	g.Delete("/rules/:id", deleteNatRule)
}

// GET /api/nat/interfaces
// Returns host network interfaces for the outInterface dropdown in the UI.
// Wrapped as { interfaces: [...] } because the frontend does `res.interfaces || []`.
func getNatInterfaces(c *fiber.Ctx) error {
	ifaces, err := nat.Get().GetNetworkInterfaces()
	if err != nil || ifaces == nil {
		ifaces = []nat.HostInterface{}
	}
	return c.JSON(fiber.Map{"interfaces": ifaces})
}

// GET /api/nat/rules
// Returns all NAT rules including auto-rules from tunnel interfaces (read-only badges).
// Wrapped as { rules: [...] } because the frontend does `res.rules || []`.
func getNatRules(c *fiber.Ctx) error {
	rules, err := nat.Get().GetRules()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if rules == nil {
		rules = []nat.NatRule{}
	}
	return c.JSON(fiber.Map{"rules": rules})
}

// POST /api/nat/rules
// Body: NatRuleInput { name, source?, sourceAliasId?, outInterface, type, toSource?, comment? }
func createNatRule(c *fiber.Ctx) error {
	var inp nat.NatRuleInput
	if err := c.BodyParser(&inp); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	r, err := nat.Get().AddRule(inp)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(r)
}

// PATCH /api/nat/rules/:id
// Supports full update OR toggle: { enabled: bool }
func updateNatRule(c *fiber.Ctx) error {
	id := c.Params("id")
	var raw map[string]any
	if err := c.BodyParser(&raw); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	// Toggle shortcut
	if enabled, ok := raw["enabled"].(bool); ok && len(raw) == 1 {
		r, err := nat.Get().ToggleRule(id, enabled)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return c.JSON(r)
	}

	// Full update — re-parse into NatRuleInput struct.
	var upd nat.NatRuleInput
	if err := c.BodyParser(&upd); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	r, err := nat.Get().UpdateRule(id, upd)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(r)
}

// DELETE /api/nat/rules/:id
func deleteNatRule(c *fiber.Ctx) error {
	if err := nat.Get().DeleteRule(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}
