// peers.go — HTTP handlers for peer CRUD within a tunnel interface.
//
// Routes (all under /api/tunnel-interfaces/:id/peers):
//
//	GET    /api/tunnel-interfaces/:id/peers
//	POST   /api/tunnel-interfaces/:id/peers
//	POST   /api/tunnel-interfaces/:id/peers/import-json        ← interconnect import
//	GET    /api/tunnel-interfaces/:id/peers/:peerId
//	PATCH  /api/tunnel-interfaces/:id/peers/:peerId
//	DELETE /api/tunnel-interfaces/:id/peers/:peerId
//	GET    /api/tunnel-interfaces/:id/peers/:peerId/config
//	GET    /api/tunnel-interfaces/:id/peers/:peerId/qrcode.svg
//	POST   /api/tunnel-interfaces/:id/peers/:peerId/enable
//	POST   /api/tunnel-interfaces/:id/peers/:peerId/disable
//	PUT    /api/tunnel-interfaces/:id/peers/:peerId/name        ← rename peer
//	PUT    /api/tunnel-interfaces/:id/peers/:peerId/address     ← update AllowedIPs
//	PUT    /api/tunnel-interfaces/:id/peers/:peerId/expireDate  ← set/clear expiry
//	POST   /api/tunnel-interfaces/:id/peers/:peerId/generateOneTimeLink
//	GET    /api/tunnel-interfaces/:id/peers/:peerId/export-json ← S2S interconnect export
package api

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

// RegisterPeers registers all /api/tunnel-interfaces/:id/peers/* routes.
func RegisterPeers(api fiber.Router) {
	g := api.Group("/tunnel-interfaces/:id/peers")

	g.Get("", listPeers)
	g.Post("", createPeer)
	g.Post("/import-json", importPeerJSON)

	g.Get("/:peerId", getPeer)
	g.Patch("/:peerId", updatePeer)
	g.Delete("/:peerId", deletePeer)

	g.Get("/:peerId/config", getPeerConfig)
	g.Get("/:peerId/qrcode.svg", getPeerQRCode)

	g.Post("/:peerId/enable", enablePeer)
	g.Post("/:peerId/disable", disablePeer)

	// Fine-grained update endpoints (ported from Node.js API).
	g.Put("/:peerId/name", renamePeer)
	g.Put("/:peerId/address", updatePeerAddress)
	g.Put("/:peerId/expireDate", updatePeerExpireDate)
	g.Post("/:peerId/generateOneTimeLink", generatePeerOneTimeLink)
	g.Get("/:peerId/export-json", exportPeerJSON)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// GET /api/tunnel-interfaces/:id/peers
// Wrapped as { peers: [...] } because the frontend does `data.peers || []`.
func listPeers(c *fiber.Ctx) error {
	peers, err := mgr().GetPeers(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	if peers == nil {
		peers = []*peer.Peer{}
	}
	return c.JSON(fiber.Map{"peers": peers})
}

// GET /api/tunnel-interfaces/:id/peers/:peerId
func getPeer(c *fiber.Ctx) error {
	p := mgr().GetPeer(c.Params("id"), c.Params("peerId"))
	if p == nil {
		return fiber.NewError(fiber.StatusNotFound, "peer not found")
	}
	return c.JSON(p)
}

// POST /api/tunnel-interfaces/:id/peers
// Body: PeerInput (name, publicKey?, privateKey?, allowedIPs?, generateKeys?, autoAllocateIP?, ...)
func createPeer(c *fiber.Ctx) error {
	ifaceID := c.Params("id")

	var inp peer.PeerInput
	if err := c.BodyParser(&inp); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	// Apply global defaults from settings when not explicitly set.
	d := peerDefaults()
	if inp.ClientAllowedIPs == "" {
		inp.ClientAllowedIPs = d.ClientAllowedIPs
	}
	if inp.PersistentKeepalive == 0 {
		inp.PersistentKeepalive = d.PersistentKeepalive
	}

	p, err := mgr().AddPeer(ifaceID, inp)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	// Wrap as { peer: {...} } because the frontend does `res.peer && res.peer.id`.
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"peer": p})
}

// POST /api/tunnel-interfaces/:id/peers/import-json
// Imports an interconnect peer from a JSON params file exported by the remote interface.
// Body: InterfaceExport JSON (name, publicKey, endpoint, address, protocol, presharedKey?, allowedIPs?)
func importPeerJSON(c *fiber.Ctx) error {
	ifaceID := c.Params("id")

	var body map[string]any
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	// Build PeerInput from the exported interface params.
	// The remote side exports its public key + endpoint; we create a peer pointing at it.
	inp := peer.PeerInput{
		PeerType: "interconnect",
	}
	if v, ok := body["name"].(string); ok {
		inp.Name = strings.TrimSpace(v)
	}
	if v, ok := body["publicKey"].(string); ok {
		inp.PublicKey = strings.TrimSpace(v)
	}
	if v, ok := body["presharedKey"].(string); ok {
		inp.PresharedKey = strings.TrimSpace(v)
	}
	if v, ok := body["endpoint"].(string); ok {
		inp.Endpoint = strings.TrimSpace(v)
	}
	// address from the export is always the remote peer's overlay IP with mask
	// (e.g. "10.255.255.1/29"). Store it as inp.Address so the peer card in the
	// UI shows the correct overlay IP — critical when multiple peers share
	// allowedIPs=0.0.0.0/0 on a transit interface.
	if v, ok := body["address"].(string); ok {
		inp.Address = strings.TrimSpace(v)
	}

	// allowedIPs from the export determines what traffic WireGuard routes through
	// this peer. For transit interfaces (disableRoutes=true) this is "0.0.0.0/0".
	// Fall back to deriving /32 from address when allowedIPs is absent (point-to-point).
	if v, ok := body["allowedIPs"].(string); ok {
		inp.AllowedIPs = strings.TrimSpace(v)
	} else if inp.Address != "" {
		// No allowedIPs in export — derive /32 from address for point-to-point peering.
		ip := strings.SplitN(inp.Address, "/", 2)[0]
		if ip != "" {
			inp.AllowedIPs = ip + "/32"
		}
	}

	// PSK is generated automatically in AddPeer when inp.PresharedKey == ""
	// (interconnect peer without PSK → AddPeer calls peer.GeneratePSK).

	p, err := mgr().AddPeer(ifaceID, inp)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"peer": p})
}

// PATCH /api/tunnel-interfaces/:id/peers/:peerId
// Body: PeerUpdate fields (name?, allowedIPs?, enabled?, endpoint?, persistentKeepalive?, expiredAt?, oneTimeLink?)
func updatePeer(c *fiber.Ctx) error {
	ifaceID := c.Params("id")
	peerID := c.Params("peerId")

	var raw map[string]any
	if err := c.BodyParser(&raw); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	upd := peer.PeerUpdate{}
	if v, ok := raw["name"].(string); ok {
		s := strings.TrimSpace(v)
		upd.Name = &s
	}
	if v, ok := raw["allowedIPs"].(string); ok {
		s := strings.TrimSpace(v)
		upd.AllowedIPs = &s
	}
	if v, ok := raw["clientAllowedIPs"].(string); ok {
		upd.ClientAllowedIPs = &v
	}
	if v, ok := raw["endpoint"].(string); ok {
		s := strings.TrimSpace(v)
		upd.Endpoint = &s
	}
	if v, ok := raw["persistentKeepalive"].(float64); ok {
		n := int(v)
		upd.PersistentKeepalive = &n
	}
	if v, ok := raw["enabled"].(bool); ok {
		upd.Enabled = &v
	}
	if v, ok := raw["expiredAt"].(string); ok {
		upd.ExpiredAt = &v
	}
	if v, ok := raw["oneTimeLink"].(string); ok {
		upd.OneTimeLink = &v
	}

	p, err := mgr().UpdatePeer(ifaceID, peerID, upd)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(p)
}

// DELETE /api/tunnel-interfaces/:id/peers/:peerId
func deletePeer(c *fiber.Ctx) error {
	if err := mgr().RemovePeer(c.Params("id"), c.Params("peerId")); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// GET /api/tunnel-interfaces/:id/peers/:peerId/config
// Returns the downloadable WireGuard client config as plain text.
func getPeerConfig(c *fiber.Ctx) error {
	config, err := mgr().GetPeerRemoteConfig(c.Params("id"), c.Params("peerId"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	c.Set("Content-Type", "text/plain; charset=utf-8")
	c.Set("Content-Disposition", `attachment; filename="wg.conf"`)
	return c.SendString(config)
}

// GET /api/tunnel-interfaces/:id/peers/:peerId/qrcode.svg
// Returns the peer config as a QR code SVG image.
func getPeerQRCode(c *fiber.Ctx) error {
	config, err := mgr().GetPeerRemoteConfig(c.Params("id"), c.Params("peerId"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}

	svg, err := peer.GenerateQRSVG(config)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "qr generation failed: "+err.Error())
	}

	c.Set("Content-Type", "image/svg+xml")
	return c.SendString(svg)
}

// POST /api/tunnel-interfaces/:id/peers/:peerId/enable
func enablePeer(c *fiber.Ctx) error {
	return togglePeer(c, true)
}

// POST /api/tunnel-interfaces/:id/peers/:peerId/disable
func disablePeer(c *fiber.Ctx) error {
	return togglePeer(c, false)
}

func togglePeer(c *fiber.Ctx, enabled bool) error {
	ifaceID := c.Params("id")
	peerID := c.Params("peerId")
	p, err := mgr().UpdatePeer(ifaceID, peerID, peer.PeerUpdate{Enabled: &enabled})
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.JSON(p)
}

// ── Fine-grained update endpoints ─────────────────────────────────────────────

// PUT /api/tunnel-interfaces/:id/peers/:peerId/name
// Body: { name: string }
// Thin wrapper over PATCH that matches the Node.js API contract.
func renamePeer(c *fiber.Ctx) error {
	ifaceID := c.Params("id")
	peerID := c.Params("peerId")

	var body struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	name := strings.TrimSpace(body.Name)
	p, err := mgr().UpdatePeer(ifaceID, peerID, peer.PeerUpdate{Name: &name})
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"peer": p})
}

// PUT /api/tunnel-interfaces/:id/peers/:peerId/address
// Body: { address: string }  — sets AllowedIPs (the peer's tunnel IP/CIDR).
// Mirrors Node.js: updatePeer(id, peerId, { allowedIPs: address }).
func updatePeerAddress(c *fiber.Ctx) error {
	ifaceID := c.Params("id")
	peerID := c.Params("peerId")

	var body struct {
		Address string `json:"address"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	addr := strings.TrimSpace(body.Address)
	p, err := mgr().UpdatePeer(ifaceID, peerID, peer.PeerUpdate{AllowedIPs: &addr})
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"peer": p})
}

// PUT /api/tunnel-interfaces/:id/peers/:peerId/expireDate
// Body: { expireDate: string | null }  — RFC3339 or "YYYY-MM-DD"; empty/null to clear.
// Mirrors Node.js: new Date(expireDate).toISOString() → expiredAt.
func updatePeerExpireDate(c *fiber.Ctx) error {
	ifaceID := c.Params("id")
	peerID := c.Params("peerId")

	var body struct {
		ExpireDate *string `json:"expireDate"` // pointer so null is distinguishable from ""
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	var expiredAt string // "" means clear
	if body.ExpireDate != nil && strings.TrimSpace(*body.ExpireDate) != "" {
		raw := strings.TrimSpace(*body.ExpireDate)
		// Try RFC3339 first, then date-only format.
		for _, layout := range []string{time.RFC3339, "2006-01-02"} {
			if t, err := time.Parse(layout, raw); err == nil {
				expiredAt = t.UTC().Format(time.RFC3339)
				break
			}
		}
		if expiredAt == "" {
			return fiber.NewError(fiber.StatusBadRequest, "invalid expireDate: expected RFC3339 or YYYY-MM-DD")
		}
	}

	p, err := mgr().UpdatePeer(ifaceID, peerID, peer.PeerUpdate{ExpiredAt: &expiredAt})
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"peer": p})
}

// POST /api/tunnel-interfaces/:id/peers/:peerId/generateOneTimeLink
// Generates a random 32-char hex one-time link token and stores it on the peer.
// The frontend then constructs a URL: /api/.../:peerId/config?token=<link>
func generatePeerOneTimeLink(c *fiber.Ctx) error {
	ifaceID := c.Params("id")
	peerID := c.Params("peerId")

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to generate link token")
	}
	link := fmt.Sprintf("%x", b)

	p, err := mgr().UpdatePeer(ifaceID, peerID, peer.PeerUpdate{OneTimeLink: &link})
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"peer": p})
}

// GET /api/tunnel-interfaces/:id/peers/:peerId/export-json
// Returns peer parameters suitable for import on the remote side (S2S workflow).
// Only available for interconnect peers — client peers are not exported this way.
// Response matches the format that POST /peers/import-json expects.
func exportPeerJSON(c *fiber.Ctx) error {
	ifaceID := c.Params("id")
	peerID := c.Params("peerId")

	t := mgr().GetInterface(ifaceID)
	if t == nil {
		return fiber.NewError(fiber.StatusNotFound, "interface not found")
	}

	p := mgr().GetPeer(ifaceID, peerID)
	if p == nil {
		return fiber.NewError(fiber.StatusNotFound, "peer not found")
	}
	if p.PeerType != "interconnect" {
		return fiber.NewError(fiber.StatusBadRequest, "export-json is only available for interconnect peers")
	}

	// Construct endpoint: WG_HOST:listenPort (mirrors exportPeerParams in Node.js TunnelInterface).
	endpoint := ""
	if host := getWGHost(); host != "" {
		endpoint = fmt.Sprintf("%s:%d", host, t.ListenPort)
	}

	clientAllowedIPs := p.ClientAllowedIPs
	if clientAllowedIPs == "" {
		clientAllowedIPs = "0.0.0.0/0"
	}

	return c.JSON(fiber.Map{
		"name":                p.Name,
		"publicKey":           p.PublicKey,
		"presharedKey":        p.PresharedKey,
		"endpoint":            endpoint,
		"persistentKeepalive": p.PersistentKeepalive,
		"allowedIPs":          p.AllowedIPs,       // this side's tunnel IP /32
		"clientAllowedIPs":    clientAllowedIPs,   // what remote will route through us
	})
}
