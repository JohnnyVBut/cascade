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
	"github.com/JohnnyVBut/cascade/internal/tunnel"
)

// RegisterNat registers all /api/nat/* routes.
func RegisterNat(api fiber.Router) {
	g := api.Group("/nat")

	g.Get("/interfaces", getNatInterfaces)
	g.Get("/rules", getNatRules)
	g.Post("/rules", createNatRule)
	g.Patch("/rules/:id", updateNatRule)
	g.Delete("/rules/:id", deleteNatRule)

	// Port Forwarding (DNAT)
	g.Get("/dnat", getDnatRules)
	g.Post("/dnat", createDnatRule)
	g.Patch("/dnat/:id", updateDnatRule)
	g.Delete("/dnat/:id", deleteDnatRule)
}

// ifaceWithLabel is the JSON shape returned by all interface-list endpoints.
// Label is the human-readable name shown in dropdowns: for WG/AWG interfaces
// it is "<id> <tunnel-name>" (e.g. "wg10-s2s Finland"), for everything else
// it equals Name.
type ifaceWithLabel struct {
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	Operstate string   `json:"operstate,omitempty"`
	Addrs     []string `json:"addrs,omitempty"`
}

// enrichIfaceLabels builds a slice of ifaceWithLabel from raw interface names,
// annotating WG/AWG tunnel interfaces with their human-readable tunnel name.
// addrsByName optionally provides IPv4 addresses per interface name.
func enrichIfaceLabels(names []string, operByName map[string]string) []ifaceWithLabel {
	return enrichIfaceLabelsWithAddrs(names, operByName, nil)
}

func enrichIfaceLabelsWithAddrs(names []string, operByName map[string]string, addrsByName map[string][]string) []ifaceWithLabel {
	nameByID := make(map[string]string)
	if tm := tunnel.Get(); tm != nil {
		for _, t := range tm.GetAllInterfaces() {
			if t.Name != "" && t.Name != t.ID {
				nameByID[t.ID] = t.Name
			}
		}
	}
	result := make([]ifaceWithLabel, 0, len(names))
	for _, n := range names {
		label := n
		if tn, ok := nameByID[n]; ok {
			label = n + " " + tn
		}
		item := ifaceWithLabel{
			Name:      n,
			Label:     label,
			Operstate: operByName[n],
		}
		if addrsByName != nil {
			item.Addrs = addrsByName[n]
		}
		result = append(result, item)
	}
	return result
}

// GET /api/nat/interfaces
// Returns host network interfaces for the outInterface dropdown in the UI.
// Wrapped as { interfaces: [...] } because the frontend does `res.interfaces || []`.
// ifaceNamesAndAddrs extracts names and builds addrsByName map from HostInterface slice.
func ifaceNamesAndAddrs(ifaces []nat.HostInterface) ([]string, map[string][]string) {
	names := make([]string, len(ifaces))
	addrs := make(map[string][]string, len(ifaces))
	for i, iface := range ifaces {
		names[i] = iface.Name
		if len(iface.Addrs) > 0 {
			addrs[iface.Name] = iface.Addrs
		}
	}
	return names, addrs
}

func getNatInterfaces(c *fiber.Ctx) error {
	ifaces, err := nat.Get().GetNetworkInterfaces()
	if err != nil || ifaces == nil {
		ifaces = []nat.HostInterface{}
	}
	names, addrs := ifaceNamesAndAddrs(ifaces)
	return c.JSON(fiber.Map{"interfaces": enrichIfaceLabelsWithAddrs(names, nil, addrs)})
}

// GET /api/nat/rules
// Returns all NAT rules including auto-rules from tunnel interfaces (read-only badges).
// Auto rules are synthesized in-memory from enabled interfaces and prepended to DB rules.
// Wrapped as { rules: [...] } because the frontend does `res.rules || []`.
func getNatRules(c *fiber.Ctx) error {
	dbRules, err := nat.Get().GetRules()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if dbRules == nil {
		dbRules = []nat.NatRule{}
	}

	// Build virtual auto-rules from tunnel interfaces.
	// Convert *tunnel.TunnelInterface → nat.IfaceInfo to avoid an import cycle
	// between the nat and tunnel packages.
	ifaces := tunnel.Get().GetAllInterfaces()
	ifaceInfos := make([]nat.IfaceInfo, 0, len(ifaces))
	for _, t := range ifaces {
		ifaceInfos = append(ifaceInfos, nat.IfaceInfo{
			ID:            t.ID,
			Name:          t.Name,
			Address:       t.Address,
			Enabled:       t.Enabled,
			NatDisabled:   t.NatDisabled,
			DisableRoutes: t.DisableRoutes,
		})
	}
	autoRules := nat.Get().GetAutoNatRules(ifaceInfos)
	if autoRules == nil {
		autoRules = []nat.NatRule{}
	}

	// Auto rules first (so they appear at the top of the table), then user-defined rules.
	all := append(autoRules, dbRules...)
	return c.JSON(fiber.Map{"rules": all})
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

// ── Port Forwarding (DNAT) handlers ──────────────────────────────────────────

// GET /api/nat/dnat
func getDnatRules(c *fiber.Ctx) error {
	rules, err := nat.Get().GetDnatRules()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"rules": rules})
}

// POST /api/nat/dnat
// Body: DnatRuleInput { name, protocol, inPort, destIP, destPort?, comment? }
func createDnatRule(c *fiber.Ctx) error {
	var inp nat.DnatRuleInput
	if err := c.BodyParser(&inp); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	r, err := nat.Get().AddDnatRule(inp)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(r)
}

// PATCH /api/nat/dnat/:id
// Supports full update OR toggle: { enabled: bool }
func updateDnatRule(c *fiber.Ctx) error {
	id := c.Params("id")
	var raw map[string]any
	if err := c.BodyParser(&raw); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	// Toggle shortcut
	if enabled, ok := raw["enabled"].(bool); ok && len(raw) == 1 {
		r, err := nat.Get().ToggleDnatRule(id, enabled)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return c.JSON(r)
	}

	var upd nat.DnatRuleInput
	if err := c.BodyParser(&upd); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	r, err := nat.Get().UpdateDnatRule(id, upd)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(r)
}

// DELETE /api/nat/dnat/:id
func deleteDnatRule(c *fiber.Ctx) error {
	if err := nat.Get().DeleteDnatRule(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}
