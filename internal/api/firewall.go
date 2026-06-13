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
	g.Post("/reorder", reorderFirewallRules)
	g.Get("/pending", getFirewallPending)
	g.Post("/apply", applyFirewallRules)
	g.Post("/discard", discardFirewallChanges)
}

// GET /api/firewall/interfaces
// Returns host network interfaces for the rule's "interface" dropdown.
// WG/AWG interfaces are enriched with tunnel name label via enrichIfaceLabels.
func getFirewallInterfaces(c *fiber.Ctx) error {
	ifaces, err := firewall.Get().GetNetworkInterfaces()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	names := make([]string, len(ifaces))
	for i, iface := range ifaces {
		names[i] = iface.Name
	}
	return c.JSON(enrichIfaceLabels(names, nil))
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
// Special: if body contains { ruleType: "separator", name: "..." } — creates a visual separator.
func createFirewallRule(c *fiber.Ctx) error {
	var raw map[string]any
	if err := c.BodyParser(&raw); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	if rt, _ := raw["ruleType"].(string); rt == "separator" {
		name, _ := raw["name"].(string)
		color, _ := raw["color"].(string)
		sep, err := firewall.Get().AddSeparator(name, color)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.Status(fiber.StatusCreated).JSON(sep)
	}
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

	// Separator update: { ruleType: "separator", name: "...", color: "..." }
	if rt, _ := raw["ruleType"].(string); rt == "separator" {
		name, _ := raw["name"].(string)
		color, _ := raw["color"].(string)
		sep, err := firewall.Get().UpdateSeparator(id, name, color)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return c.JSON(sep)
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

// GET /api/firewall/pending
// Returns { hasPendingChanges: bool } — whether draft differs from applied snapshot.
func getFirewallPending(c *fiber.Ctx) error {
	has, err := firewall.Get().HasPendingChanges()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"hasPendingChanges": has})
}

// POST /api/firewall/apply
// Copies draft → applied snapshot and rebuilds iptables chains.
func applyFirewallRules(c *fiber.Ctx) error {
	if err := firewall.Get().ApplyRules(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// POST /api/firewall/discard
// Reverts draft to the applied snapshot (no kernel change).
func discardFirewallChanges(c *fiber.Ctx) error {
	if err := firewall.Get().DiscardChanges(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// POST /api/firewall/reorder
// Body: { ids: ["id1", "id2", ...] } — full ordered list of all rule IDs.
// The list must contain exactly the current rule IDs (no extras, no missing).
const maxFirewallRules = 500 // reasonable upper bound to prevent lock-amplification DoS

func reorderFirewallRules(c *fiber.Ctx) error {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	if len(body.IDs) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "ids must not be empty")
	}
	if len(body.IDs) > maxFirewallRules {
		return fiber.NewError(fiber.StatusBadRequest, "too many ids")
	}
	if err := firewall.Get().ReorderRules(body.IDs); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
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
