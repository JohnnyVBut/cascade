// Package api — compatibility shims for legacy Node.js-era endpoints.
//
// The embedded frontend (internal/frontend/www) still calls several endpoints
// that existed in the original Node.js/h3 server but have no direct equivalent
// in the Go/Fiber API.  Rather than patching the frontend JS, we serve minimal
// stub responses so the UI starts cleanly without error toasts.
//
// All stubs are read-only GET handlers that return safe defaults.
// Write operations on legacy paths (POST/PUT/DELETE /api/wireguard/*) return
// 501 Not Implemented so that destructive calls fail loudly.
package api

import (
	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/nat"
)

// RegisterCompat wires the legacy shim routes onto the given router group.
// Must be called BEFORE the auth middleware so that unauthenticated startup
// calls (getLang, getRelease) also receive a proper JSON response.
func RegisterCompat(r fiber.Router) {
	// ── Unauthenticated stubs (called before login) ─────────────────────────

	// GET /api/lang — returns the UI locale stored in ENV/settings.
	// Stub: always return "en"; the browser falls back to localStorage anyway.
	r.Get("/lang", func(c *fiber.Ctx) error {
		return c.JSON("en")
	})

	// GET /api/release — current release version integer.
	// Return a large sentinel so currentRelease >= latestRelease.version is
	// always true and the "new release available" banner never appears.
	r.Get("/release", func(c *fiber.Ctx) error {
		return c.JSON(999999)
	})

	// GET /api/remember-me — whether the "remember me" checkbox is shown.
	r.Get("/remember-me", func(c *fiber.Ctx) error {
		return c.JSON(true)
	})

	// ── UI-feature-flag stubs ────────────────────────────────────────────────

	r.Get("/ui-traffic-stats", func(c *fiber.Ctx) error {
		return c.JSON(true)
	})

	r.Get("/ui-chart-type", func(c *fiber.Ctx) error {
		return c.JSON(1)
	})

	r.Get("/wg-enable-one-time-links", func(c *fiber.Ctx) error {
		return c.JSON(false)
	})

	r.Get("/ui-sort-clients", func(c *fiber.Ctx) error {
		return c.JSON(false)
	})

	r.Get("/wg-enable-expire-time", func(c *fiber.Ctx) error {
		return c.JSON(false)
	})

	r.Get("/ui-avatar-settings", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"dicebear": nil, "gravatar": false})
	})
}

// RegisterCompatAuth wires legacy stubs that require authentication.
// Called AFTER the auth middleware group is set up.
func RegisterCompatAuth(r fiber.Router) {
	// GET /api/wireguard/client — the old admin-tunnel client list.
	// The Administration tab calls this every second via refresh().
	// Return an empty array so the page renders without errors.
	// Full implementation is deferred (KNOWN-2: AdminInstance).
	r.Get("/wireguard/client", func(c *fiber.Ctx) error {
		return c.JSON([]fiber.Map{})
	})

	// Catch-all for other legacy wireguard write operations — fail loudly
	// rather than silently so future callers notice these are not implemented.
	r.All("/wireguard/*", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusNotImplemented,
			"legacy wireguard endpoint not implemented in Go API")
	})

	// GET /api/system/interfaces — host network interfaces for the gateway form.
	// Reuses the same ip-link-show parser from the NAT manager.
	// Returns { interfaces: [...] } because the frontend does `res.interfaces || []`.
	r.Get("/system/interfaces", func(c *fiber.Ctx) error {
		ifaces, err := nat.Get().GetNetworkInterfaces()
		if err != nil || ifaces == nil {
			ifaces = []nat.HostInterface{}
		}
		return c.JSON(fiber.Map{"interfaces": ifaces})
	})
}
