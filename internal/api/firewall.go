// firewall.go — HTTP handlers for FirewallManager (filter rules + PBR).
//
// Routes:
//
//	GET    /api/firewall/interfaces   ← host interfaces for the rule's interface field
//	GET    /api/firewall/rules
//	POST   /api/firewall/rules
//	PATCH  /api/firewall/rules/:id
//	DELETE /api/firewall/rules/:id
//	POST   /api/firewall/rules/:id/move  ← { direction: "up"|"down" }
package api

import (
	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/firewall"
)

// RegisterFirewall registers all /api/firewall/* routes.
func RegisterFirewall(api fiber.Router) {
	g := api.Group("/firewall")

	g.Get("/interfaces", getFirewallInterfaces)
	g.Get("/rules", getFirewallRules)
	g.Post("/rules", createFirewallRule)
	g.Patch("/rules/:id", updateFirewallRule)
	g.Delete("/rules/:id", deleteFirewallRule)
	g.Post("/rules/:id/move", moveFirewallRule)
}

// GET /api/firewall/interfaces
// Returns host network interfaces for the rule's "interface" dropdown.
func getFirewallInterfaces(c *fiber.Ctx) error {
	ifaces, err := firewall.Get().GetNetworkInterfaces()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(ifaces)
}

// GET /api/firewall/rules
// Frontend does: Array.isArray(res) ? res : (res.rules || [])
// Return a bare (non-nil) array so Array.isArray passes and no TypeError on nil.
func getFirewallRules(c *fiber.Ctx) error {
	rules, err := firewall.Get().GetRules()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if rules == nil {
		rules = []firewall.Rule{}
	}
	return c.JSON(rules)
}

// POST /api/firewall/rules
// Body: RuleInput { name, interface, protocol, source, destination, action, gatewayId?, ... }
func createFirewallRule(c *fiber.Ctx) error {
	var inp firewall.RuleInput
	if err := c.BodyParser(&inp); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	r, err := firewall.Get().AddRule(inp)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(r)
}

// PATCH /api/firewall/rules/:id
// Supports full update OR toggle: { enabled: bool }
func updateFirewallRule(c *fiber.Ctx) error {
	id := c.Params("id")
	var raw map[string]any
	if err := c.BodyParser(&raw); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	// Toggle shortcut
	if enabled, ok := raw["enabled"].(bool); ok && len(raw) == 1 {
		r, err := firewall.Get().ToggleRule(id, enabled)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return c.JSON(r)
	}

	var upd firewall.RuleInput
	if err := c.BodyParser(&upd); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	r, err := firewall.Get().UpdateRule(id, upd)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(r)
}

// DELETE /api/firewall/rules/:id
func deleteFirewallRule(c *fiber.Ctx) error {
	if err := firewall.Get().DeleteRule(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// POST /api/firewall/rules/:id/move
// Body: { direction: "up" | "down" }
func moveFirewallRule(c *fiber.Ctx) error {
	var body struct {
		Direction string `json:"direction"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	if body.Direction != "up" && body.Direction != "down" {
		return fiber.NewError(fiber.StatusBadRequest, `direction must be "up" or "down"`)
	}
	if _, err := firewall.Get().MoveRule(c.Params("id"), body.Direction); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}
