// gateways.go — HTTP handlers for GatewayManager (gateways + gateway groups).
//
// Routes:
//
//	GET    /api/gateways
//	POST   /api/gateways
//	GET    /api/gateways/:id
//	PATCH  /api/gateways/:id
//	DELETE /api/gateways/:id
//
//	GET    /api/gateway-groups
//	POST   /api/gateway-groups
//	GET    /api/gateway-groups/:id
//	PATCH  /api/gateway-groups/:id
//	DELETE /api/gateway-groups/:id
package api

import (
	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/gateway"
)

// RegisterGateways registers all /api/gateways/* and /api/gateway-groups/* routes.
func RegisterGateways(api fiber.Router) {
	// ── Gateways ──────────────────────────────────────────────────────────────
	gw := api.Group("/gateways")

	gw.Get("", listGateways)
	gw.Post("", createGateway)
	gw.Get("/:id", getGateway)
	gw.Patch("/:id", updateGateway)
	gw.Delete("/:id", deleteGateway)

	// ── Gateway Groups ─────────────────────────────────────────────────────────
	gg := api.Group("/gateway-groups")

	gg.Get("", listGatewayGroups)
	gg.Post("", createGatewayGroup)
	gg.Get("/:id", getGatewayGroup)
	gg.Patch("/:id", updateGatewayGroup)
	gg.Delete("/:id", deleteGatewayGroup)
}

// ── Gateway handlers ──────────────────────────────────────────────────────────

// GET /api/gateways
// Returns all gateways with live monitoring status (latency, packet loss, HTTP).
// Wrapped as { gateways: [...] } because the frontend does `res.gateways || []`.
func listGateways(c *fiber.Ctx) error {
	gws, err := gateway.Get().GetAllGatewaysWithStatus()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if gws == nil {
		gws = []gateway.GatewayWithStatus{}
	}
	return c.JSON(fiber.Map{"gateways": gws})
}

// GET /api/gateways/:id
func getGateway(c *fiber.Ctx) error {
	gw, err := gateway.Get().GetGateway(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if gw == nil {
		return fiber.NewError(fiber.StatusNotFound, "gateway not found")
	}
	return c.JSON(gw)
}

// POST /api/gateways
// Body: GatewayInput { name, interface, gatewayIP, monitorAddress?, monitorInterval?, ... }
func createGateway(c *fiber.Ctx) error {
	var inp gateway.GatewayInput
	if err := c.BodyParser(&inp); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	gw, err := gateway.Get().CreateGateway(inp)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(gw)
}

// PATCH /api/gateways/:id
func updateGateway(c *fiber.Ctx) error {
	var inp gateway.GatewayInput
	if err := c.BodyParser(&inp); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	gw, err := gateway.Get().UpdateGateway(c.Params("id"), inp)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(gw)
}

// DELETE /api/gateways/:id
func deleteGateway(c *fiber.Ctx) error {
	if err := gateway.Get().DeleteGateway(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Gateway group handlers ─────────────────────────────────────────────────────

// GET /api/gateway-groups
// Wrapped as { groups: [...] } because the frontend does `res.groups || []`.
func listGatewayGroups(c *fiber.Ctx) error {
	groups, err := gateway.Get().GetGroups()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if groups == nil {
		groups = []gateway.GatewayGroup{}
	}
	return c.JSON(fiber.Map{"groups": groups})
}

// GET /api/gateway-groups/:id
func getGatewayGroup(c *fiber.Ctx) error {
	group, err := gateway.Get().GetGroup(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if group == nil {
		return fiber.NewError(fiber.StatusNotFound, "gateway group not found")
	}
	return c.JSON(group)
}

// POST /api/gateway-groups
// Body: GatewayGroupInput { name, trigger, members: [{gatewayId, tier}], description? }
func createGatewayGroup(c *fiber.Ctx) error {
	var inp gateway.GatewayGroupInput
	if err := c.BodyParser(&inp); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	group, err := gateway.Get().CreateGroup(inp)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(group)
}

// PATCH /api/gateway-groups/:id
func updateGatewayGroup(c *fiber.Ctx) error {
	var inp gateway.GatewayGroupInput
	if err := c.BodyParser(&inp); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	group, err := gateway.Get().UpdateGroup(c.Params("id"), inp)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(group)
}

// DELETE /api/gateway-groups/:id
func deleteGatewayGroup(c *fiber.Ctx) error {
	if err := gateway.Get().DeleteGroup(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}
