// Package api contains all HTTP route handlers.
// Each file corresponds to one resource group (settings, interfaces, peers, …).
// Handlers are pure functions — no state, all state lives in internal/* packages.
package api

import (
	"log"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/awgparams"
	"github.com/JohnnyVBut/cascade/internal/settings"
)

// getHostname returns the real host hostname.
// Docker containers get a random hash from os.Hostname(), so we first try
// /host_hostname which is mounted from the host's /etc/hostname (read-only).
func getHostname() string {
	if b, err := os.ReadFile("/host_hostname"); err == nil {
		if h := strings.TrimSpace(string(b)); h != "" {
			return h
		}
	}
	h, _ := os.Hostname()
	return h
}

// SettingsResponse wraps GlobalSettings and adds runtime-only fields
// (hostname, resolvedPublicIP, publicIPWarning) that are not stored in the DB.
type SettingsResponse struct {
	settings.GlobalSettings
	Hostname         string `json:"hostname"`
	ResolvedPublicIP string `json:"resolvedPublicIP"`
	PublicIPWarning  string `json:"publicIPWarning"`
}

// RegisterSettings registers all /api/settings and /api/templates routes.
// Must be called after db.Init().
//
// Routes registered:
//
//	GET  /api/settings
//	PUT  /api/settings
//	GET  /api/templates
//	POST /api/templates
//	GET  /api/templates/:id
//	PUT  /api/templates/:id
//	DELETE /api/templates/:id
//	POST /api/templates/:id/set-default
//	POST /api/templates/:id/apply
//	POST /api/templates/generate         ← stub until AwgParamGenerator is ported
func RegisterSettings(api fiber.Router) {
	// ── Global Settings ───────────────────────────────────────────────────────

	// GET /api/settings — return current global settings + runtime info
	api.Get("/settings", func(c *fiber.Ctx) error {
		s, err := settings.GetSettings()
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		hostname := getHostname()
		resolvedIP, ipWarn := settings.ResolvePublicIP(s.PublicIPMode, s.PublicIPManual)
		return c.JSON(SettingsResponse{
			GlobalSettings:   *s,
			Hostname:         hostname,
			ResolvedPublicIP: resolvedIP,
			PublicIPWarning:  ipWarn,
		})
	})

	// PUT /api/settings — partial update
	// Body: { dns?, defaultPersistentKeepalive?, defaultClientAllowedIPs?,
	//         gatewayWindowSeconds?, gatewayHealthyThreshold?, gatewayDegradedThreshold?,
	//         routerName?, publicIPMode?, publicIPManual? }
	api.Put("/settings", func(c *fiber.Ctx) error {
		var body map[string]any
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "Invalid JSON body")
		}

		updated, err := settings.UpdateSettings(body)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}

		// Invalidate public IP cache if mode or manual IP changed.
		if _, ok := body["publicIPMode"]; ok {
			settings.InvalidateIPCache()
		}
		if _, ok := body["publicIPManual"]; ok {
			settings.InvalidateIPCache()
		}

		hostname := getHostname()
		resolvedIP, ipWarn := settings.ResolvePublicIP(updated.PublicIPMode, updated.PublicIPManual)

		log.Println("settings: updated")
		return c.JSON(SettingsResponse{
			GlobalSettings:   *updated,
			Hostname:         hostname,
			ResolvedPublicIP: resolvedIP,
			PublicIPWarning:  ipWarn,
		})
	})

	// ── AWG2 Templates ────────────────────────────────────────────────────────

	// GET /api/templates — list all templates
	api.Get("/templates", func(c *fiber.Ctx) error {
		list, err := settings.GetTemplates()
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		// Return nil as [] not null (matches Node.js behaviour)
		if list == nil {
			list = []settings.Template{}
		}
		return c.JSON(fiber.Map{"templates": list})
	})

	// POST /api/templates/generate — generate AWG2 obfuscation params
	// Registered BEFORE /:id routes so Fiber doesn't interpret "generate" as an id.
	// Body: { profile?, intensity?, host?, iterCount?, jc?, saveName? }
	// Returns: { params, profiles } | { params, profiles, template } if saveName provided
	api.Post("/templates/generate", func(c *fiber.Ctx) error {
		var body struct {
			Profile   string  `json:"profile"`
			Intensity string  `json:"intensity"`
			Host      string  `json:"host"`
			IterCount int     `json:"iterCount"`
			Jc        int     `json:"jc"`
			SaveName  string  `json:"saveName"`
		}
		// Body is optional — ignore parse errors, use zero values → defaults
		_ = c.BodyParser(&body)

		params := awgparams.Generate(awgparams.Options{
			Profile:   body.Profile,
			Intensity: body.Intensity,
			Host:      body.Host,
			IterCount: body.IterCount,
			Jc:        body.Jc,
		})

		if body.SaveName != "" {
			tmpl, err := settings.CreateTemplate(settings.Template{
				Name: body.SaveName,
				Jc: params.Jc, Jmin: params.Jmin, Jmax: params.Jmax,
				S1: params.S1, S2: params.S2, S3: params.S3, S4: params.S4,
				H1: params.H1, H2: params.H2, H3: params.H3, H4: params.H4,
				I1: params.I1, I2: params.I2, I3: params.I3, I4: params.I4, I5: params.I5,
			})
			if err != nil {
				return fiber.NewError(fiber.StatusBadRequest, err.Error())
			}
			log.Printf("awgparams: generated + saved as %q (profile=%s)", body.SaveName, params.Profile)
			return c.JSON(fiber.Map{
				"params":   params,
				"profiles": awgparams.Profiles,
				"template": tmpl,
			})
		}

		log.Printf("awgparams: generated profile=%s intensity=%s", params.Profile, body.Intensity)
		return c.JSON(fiber.Map{
			"params":   params,
			"profiles": awgparams.Profiles,
		})
	})

	// POST /api/templates — create new template
	// Body: { name, isDefault?, jc, jmin, jmax, s1-s4, h1-h4, i1-i5 }
	api.Post("/templates", func(c *fiber.Ctx) error {
		var body settings.Template
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "Invalid JSON body")
		}
		if body.Name == "" {
			return fiber.NewError(fiber.StatusBadRequest, "Template name is required")
		}

		tmpl, err := settings.CreateTemplate(body)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}

		log.Printf("settings: template created %s (%s)", tmpl.ID, tmpl.Name)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"template": tmpl})
	})

	// GET /api/templates/:id — get single template
	api.Get("/templates/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		tmpl, err := settings.GetTemplate(id)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		if tmpl == nil {
			return fiber.NewError(fiber.StatusNotFound, "Template not found")
		}
		return c.JSON(fiber.Map{"template": tmpl})
	})

	// PUT /api/templates/:id — update template (partial)
	api.Put("/templates/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")

		// Parse as map for partial update support
		var updates map[string]any
		if err := c.BodyParser(&updates); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "Invalid JSON body")
		}

		tmpl, err := settings.UpdateTemplate(id, updates)
		if err != nil {
			if err.Error() == "template not found" {
				return fiber.NewError(fiber.StatusNotFound, err.Error())
			}
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}

		log.Printf("settings: template updated %s", id)
		return c.JSON(fiber.Map{"template": tmpl})
	})

	// DELETE /api/templates/:id — delete template
	api.Delete("/templates/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		if err := settings.DeleteTemplate(id); err != nil {
			if err.Error() == "template not found" {
				return fiber.NewError(fiber.StatusNotFound, err.Error())
			}
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}

		log.Printf("settings: template deleted %s", id)
		return c.JSON(fiber.Map{"success": true})
	})

	// POST /api/templates/:id/set-default — mark template as default
	api.Post("/templates/:id/set-default", func(c *fiber.Ctx) error {
		id := c.Params("id")
		tmpl, err := settings.SetDefaultTemplate(id)
		if err != nil {
			if err.Error() == "template not found" {
				return fiber.NewError(fiber.StatusNotFound, err.Error())
			}
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}

		log.Printf("settings: template set as default %s", id)
		return c.JSON(fiber.Map{"template": tmpl})
	})

	// POST /api/templates/:id/apply — get AWG2 params from template (H1-H4 as-is, FIX-4)
	api.Post("/templates/:id/apply", func(c *fiber.Ctx) error {
		id := c.Params("id")
		params, err := settings.ApplyTemplate(id)
		if err != nil {
			if err.Error() == "template not found" {
				return fiber.NewError(fiber.StatusNotFound, err.Error())
			}
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(fiber.Map{"settings": params})
	})
}
