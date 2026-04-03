// interfaces.go — HTTP handlers for tunnel interface CRUD and lifecycle.
//
// Routes:
//
//	GET    /api/tunnel-interfaces
//	POST   /api/tunnel-interfaces
//	POST   /api/tunnel-interfaces/quick-create   ← MUST be registered before /:id
//	GET    /api/tunnel-interfaces/:id
//	PATCH  /api/tunnel-interfaces/:id
//	DELETE /api/tunnel-interfaces/:id
//	POST   /api/tunnel-interfaces/:id/start
//	POST   /api/tunnel-interfaces/:id/stop
//	POST   /api/tunnel-interfaces/:id/restart
//	GET    /api/tunnel-interfaces/:id/export-params
//	GET    /api/tunnel-interfaces/:id/export-obfuscation
//	GET    /api/tunnel-interfaces/:id/backup    ← download interface+peers as JSON
//	PUT    /api/tunnel-interfaces/:id/restore   ← restore peers from JSON backup
package api

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/firewall"
	"github.com/JohnnyVBut/cascade/internal/peer"
	"github.com/JohnnyVBut/cascade/internal/settings"
	"github.com/JohnnyVBut/cascade/internal/tunnel"
	"github.com/JohnnyVBut/cascade/internal/validate"
)

// RegisterInterfaces registers all /api/tunnel-interfaces/* routes.
func RegisterInterfaces(api fiber.Router) {
	g := api.Group("/tunnel-interfaces")

	g.Get("", listInterfaces)
	g.Post("", createInterface)

	// quick-create MUST be registered before /:id to avoid Fiber routing
	// the literal path segment "quick-create" as a parameter value.
	g.Post("/quick-create", quickCreateInterface)

	g.Get("/:id", getInterface)
	g.Patch("/:id", updateInterface)
	g.Delete("/:id", deleteInterface)

	g.Post("/:id/start", startInterface)
	g.Post("/:id/stop", stopInterface)
	g.Post("/:id/restart", restartInterface)

	g.Get("/:id/export-params", exportInterfaceParams)
	g.Get("/:id/export-obfuscation", exportObfuscation)

	g.Get("/:id/backup", backupInterface)
	g.Put("/:id/restore", restoreInterface)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func mgr() *tunnel.Manager {
	return tunnel.Get()
}

// ifaceJSON builds the JSON-serialisable view of a TunnelInterface.
// PrivateKey is always excluded; peers slice is included if withPeers=true.
func ifaceJSON(t *tunnel.TunnelInterface, withPeers bool) fiber.Map {
	m := fiber.Map{
		"id":            t.ID,
		"name":          t.Name,
		"address":       t.Address,
		"listenPort":    t.ListenPort,
		"protocol":      t.Protocol,
		"enabled":       t.Enabled,
		"disableRoutes": t.DisableRoutes,
		"natDisabled":   t.NatDisabled,
		"publicKey":     t.PublicKey,
		"settings":      t.AWG2,
		"createdAt":     t.CreatedAt,
		"peerCount":     t.PeerCount(),
	}
	if withPeers {
		m["peers"] = t.GetAllPeers()
	}
	return m
}

// getWGHost returns the public host/IP used for endpoint construction.
// Priority:
// getWGHost resolves the server's public hostname/IP.
// Delegates to settings.GetWGHost() with WG_HOST env as optional override.
// Priority: WG_HOST env → Settings UI manual → auto-detect.
func getWGHost() string {
	return settings.GetWGHost(os.Getenv("WG_HOST"))
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// GET /api/tunnel-interfaces
// Returns all interfaces with their peers.
// Wrapped as { interfaces: [...] } because the frontend does `data.interfaces || []`.
func listInterfaces(c *fiber.Ctx) error {
	ifaces := mgr().GetAllInterfaces()
	out := make([]fiber.Map, 0, len(ifaces))
	for _, t := range ifaces {
		out = append(out, ifaceJSON(t, true))
	}
	return c.JSON(fiber.Map{"interfaces": out})
}

// GET /api/tunnel-interfaces/:id
func getInterface(c *fiber.Ctx) error {
	t := mgr().GetInterface(c.Params("id"))
	if t == nil {
		return fiber.NewError(fiber.StatusNotFound, "interface not found")
	}
	return c.JSON(ifaceJSON(t, true))
}

// POST /api/tunnel-interfaces
// Body: { name, protocol?, address?, listenPort?, disableRoutes?, settings? }
func createInterface(c *fiber.Ctx) error {
	var body struct {
		Name          string              `json:"name"`
		Protocol      string              `json:"protocol"`
		Address       string              `json:"address"`
		ListenPort    int                 `json:"listenPort"`
		DisableRoutes bool                `json:"disableRoutes"`
		AWG2          *peer.AWG2Settings  `json:"settings"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	addr := strings.TrimSpace(body.Address)
	if addr != "" {
		if err := validate.CIDR(addr); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "address: "+err.Error())
		}
	}

	t, err := mgr().CreateInterface(tunnel.CreateInput{
		Name:          strings.TrimSpace(body.Name),
		Protocol:      body.Protocol,
		Address:       addr,
		ListenPort:    body.ListenPort,
		DisableRoutes: body.DisableRoutes,
		AWG2:          body.AWG2,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(ifaceJSON(t, false))
}

// POST /api/tunnel-interfaces/quick-create
// Body: { name?: string, protocol?: string }
// Creates and starts a client interface (disableRoutes=false) in one step.
// Address and port are auto-assigned from SubnetPool / PortPool settings.
// AWG2 params come from the default template, or a random profile if no default is set.
//
// Response: { interface: {...}, started: bool, startError?: string }
// HTTP 200: always (even if start failed), so the UI can show the toast regardless.
// HTTP 400: if creation itself failed (pool exhausted, key-gen error, etc.).
func quickCreateInterface(c *fiber.Ctx) error {
	var body struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	result, err := mgr().QuickCreate(strings.TrimSpace(body.Name), body.Protocol)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	// Rebuild firewall PBR routing tables now that the interface is up.
	// Same pattern as startInterface (FIX-GO-9): wg-quick up creates the
	// kernel interface, so "ip route replace ... dev wgX table N" can succeed.
	if result.Started {
		if err := firewall.Get().RebuildChains(); err != nil {
			log.Printf("firewall rebuildChains after quick-create %s: %v",
				result.Interface.ID, err)
		}
	}

	resp := fiber.Map{
		"interface": ifaceJSON(result.Interface, false),
		"started":   result.Started,
	}
	if result.StartError != nil {
		resp["startError"] = result.StartError.Error()
	}
	return c.JSON(resp)
}

// PATCH /api/tunnel-interfaces/:id
// Body: { name?, address?, listenPort?, disableRoutes?, settings? }
// Applies only the fields that are present (non-nil) in the body.
func updateInterface(c *fiber.Ctx) error {
	id := c.Params("id")

	// Parse into a map so we can distinguish "field absent" from "field = zero value".
	var raw map[string]any
	if err := c.BodyParser(&raw); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	upd := tunnel.InterfaceUpdate{}

	if v, ok := raw["name"].(string); ok {
		s := strings.TrimSpace(v)
		upd.Name = &s
	}
	if v, ok := raw["address"].(string); ok {
		s := strings.TrimSpace(v)
		if err := validate.CIDR(s); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "address: "+err.Error())
		}
		upd.Address = &s
	}
	if v, ok := raw["listenPort"].(float64); ok {
		n := int(v)
		upd.ListenPort = &n
	}
	if v, ok := raw["disableRoutes"].(bool); ok {
		upd.DisableRoutes = &v
	}
	if v, ok := raw["natDisabled"].(bool); ok {
		upd.NatDisabled = &v
	}
	if v, ok := raw["settings"]; ok && v != nil {
		// Re-marshal → unmarshal into AWG2Settings.
		a, err := mapToAWG2(v)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid settings: "+err.Error())
		}
		upd.AWG2 = a
	}

	t, err := mgr().UpdateInterface(id, upd)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(ifaceJSON(t, true))
}

// DELETE /api/tunnel-interfaces/:id
func deleteInterface(c *fiber.Ctx) error {
	if err := mgr().DeleteInterface(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// POST /api/tunnel-interfaces/:id/start
func startInterface(c *fiber.Ctx) error {
	t, err := mgr().StartInterface(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	// Rebuild firewall PBR routing after interface comes up.
	// wg-quick up creates the interface → "ip route replace ... dev wgX table N"
	// can now succeed (FIX-GO-9).
	if err := firewall.Get().RebuildChains(); err != nil {
		log.Printf("firewall rebuildChains after start %s: %v", c.Params("id"), err)
	}
	return c.JSON(fiber.Map{"interface": ifaceJSON(t, false)})
}

// POST /api/tunnel-interfaces/:id/stop
func stopInterface(c *fiber.Ctx) error {
	t, err := mgr().StopInterface(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"interface": ifaceJSON(t, false)})
}

// POST /api/tunnel-interfaces/:id/restart
func restartInterface(c *fiber.Ctx) error {
	t, err := mgr().RestartInterface(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	// wg-quick down removes all routes from the interface including custom-table
	// routes used by PBR (e.g. "default via X dev wgY table 1000").
	// Rebuild firewall chains so those routes are re-added (FIX-GO-9).
	if err := firewall.Get().RebuildChains(); err != nil {
		log.Printf("firewall rebuildChains after restart %s: %v", c.Params("id"), err)
	}
	return c.JSON(fiber.Map{"interface": ifaceJSON(t, false)})
}

// GET /api/tunnel-interfaces/:id/export-params
// Returns this interface's parameters for S2S interconnect import on the remote side.
func exportInterfaceParams(c *fiber.Ctx) error {
	t := mgr().GetInterface(c.Params("id"))
	if t == nil {
		return fiber.NewError(fiber.StatusNotFound, "interface not found")
	}
	exp := t.ExportInterfaceParams(getWGHost())
	return c.JSON(exp)
}

// GET /api/tunnel-interfaces/:id/export-obfuscation
// Returns AWG2 obfuscation parameters for copying to the remote side.
func exportObfuscation(c *fiber.Ctx) error {
	t := mgr().GetInterface(c.Params("id"))
	if t == nil {
		return fiber.NewError(fiber.StatusNotFound, "interface not found")
	}
	params, err := t.ExportObfuscationParams()
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(params)
}

// GET /api/tunnel-interfaces/:id/backup
// Downloads the interface config and all peers as a single JSON file.
// The file can be restored via PUT /restore.
func backupInterface(c *fiber.Ctx) error {
	id := c.Params("id")

	t := mgr().GetInterface(id)
	if t == nil {
		return fiber.NewError(fiber.StatusNotFound, "interface not found")
	}

	peers := t.GetAllPeers()
	if peers == nil {
		peers = []*peer.Peer{}
	}

	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, id))
	c.Set("Content-Type", "application/json")
	return c.JSON(fiber.Map{
		"interface": ifaceJSON(t, false),
		"peers":     peers,
	})
}

// PUT /api/tunnel-interfaces/:id/restore
// Restores peers from a JSON backup produced by GET /backup.
// All existing peers on the interface are removed first, then backup peers are re-created.
// Body: { file: { peers: [...] } }
func restoreInterface(c *fiber.Ctx) error {
	id := c.Params("id")

	var body struct {
		File struct {
			Peers []peer.PeerInput `json:"peers"`
		} `json:"file"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	if body.File.Peers == nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid backup: missing peers array")
	}

	t := mgr().GetInterface(id)
	if t == nil {
		return fiber.NewError(fiber.StatusNotFound, "interface not found")
	}

	// Remove all existing peers first.
	existing, _ := mgr().GetPeers(id)
	for _, p := range existing {
		_ = mgr().RemovePeer(id, p.ID)
	}

	// Re-create peers from backup.
	// Force GenerateKeys=false regardless of what the backup file contains —
	// a malicious or corrupted backup with "generateKeys":true would silently
	// discard all backed-up keys and create peers with freshly generated ones,
	// making the restored config non-functional without any visible error.
	for _, inp := range body.File.Peers {
		inp.GenerateKeys = false
		if _, err := mgr().AddPeer(id, inp); err != nil {
			// Log and continue — partial restore is better than aborting.
			fmt.Printf("restore: AddPeer %q failed: %v\n", inp.Name, err)
		}
	}

	t = mgr().GetInterface(id)
	return c.JSON(fiber.Map{"interface": ifaceJSON(t, true)})
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// mapToAWG2 converts an arbitrary map[string]any (from JSON) to *peer.AWG2Settings.
func mapToAWG2(v any) (*peer.AWG2Settings, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fiber.NewError(fiber.StatusBadRequest, "settings must be an object")
	}
	a := &peer.AWG2Settings{}
	if n, ok := m["jc"].(float64); ok {
		a.Jc = int(n)
	}
	if n, ok := m["jmin"].(float64); ok {
		a.Jmin = int(n)
	}
	if n, ok := m["jmax"].(float64); ok {
		a.Jmax = int(n)
	}
	if n, ok := m["s1"].(float64); ok {
		a.S1 = int(n)
	}
	if n, ok := m["s2"].(float64); ok {
		a.S2 = int(n)
	}
	if n, ok := m["s3"].(float64); ok {
		a.S3 = int(n)
	}
	if n, ok := m["s4"].(float64); ok {
		a.S4 = int(n)
	}
	strField := func(key string) string {
		if s, ok := m[key].(string); ok {
			return s
		}
		return ""
	}
	a.H1 = strField("h1")
	a.H2 = strField("h2")
	a.H3 = strField("h3")
	a.H4 = strField("h4")
	a.I1 = strField("i1")
	a.I2 = strField("i2")
	a.I3 = strField("i3")
	a.I4 = strField("i4")
	a.I5 = strField("i5")
	return a, nil
}

// peerDefaults returns global peer defaults from settings (DNS, clientAllowedIPs, keepalive).
// Falls back to sane defaults if settings are unavailable.
func peerDefaults() *settings.PeerDefaults {
	d, err := settings.GetPeerDefaults()
	if err != nil {
		return &settings.PeerDefaults{
			DNS:                 "1.1.1.1, 8.8.8.8",
			PersistentKeepalive: 25,
			ClientAllowedIPs:    "0.0.0.0/0, ::/0",
		}
	}
	return d
}
