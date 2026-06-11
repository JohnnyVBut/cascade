// remotes.go — HTTP handlers for remote Cascade server management.
//
// Routes:
//
//	GET    /api/remotes              — list registered remote servers
//	POST   /api/remotes              — add remote (login→token→logout)
//	DELETE /api/remotes/:id          — remove remote
//	POST   /api/remotes/:id/test     — test connectivity (ping)
//	ALL    /api/remotes/:id/proxy/*  — proxy request to remote server
package api

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/remoteclient"
	"github.com/JohnnyVBut/cascade/internal/remotes"
)

// proxyClient is a shared HTTP client for proxy requests.
// Timeout is intentionally short (5 s) to prevent goroutine pile-up when the
// remote is temporarily unreachable. The browser polls every second, so a 30 s
// timeout would accumulate dozens of blocked goroutines before the first one fails.
var proxyClient = &http.Client{Timeout: 5 * time.Second}

// RegisterRemotes registers all /api/remotes/* routes.
func RegisterRemotes(api fiber.Router) {
	g := api.Group("/remotes")
	g.Get("/", listRemotes)
	g.Post("/", addRemote)
	g.Delete("/:id", deleteRemote)
	g.Post("/:id/test", testRemote)
	g.All("/:id/proxy/*", proxyRemote)
}

// GET /api/remotes
func listRemotes(c *fiber.Ctx) error {
	list, err := remotes.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"remotes": list})
}

// POST /api/remotes
// Body: { name, url, username, password }
// Connects to the remote, obtains an API token, stores it.
func addRemote(c *fiber.Ctx) error {
	var body struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	if body.Name == "" || body.URL == "" || body.Username == "" || body.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name, url, username and password are required")
	}

	// Validate URL — must be http:// or https://, no localhost/link-local (SSRF guard).
	if err := validateRemoteURL(body.URL); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	// Login → create token → logout on remote.
	token, err := remoteclient.ObtainToken(body.URL, body.Username, body.Password)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, fmt.Sprintf("could not obtain token from remote: %s", err.Error()))
	}

	remote, err := remotes.Add(body.Name, body.URL, token)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"remote": remote})
}

// DELETE /api/remotes/:id
func deleteRemote(c *fiber.Ctx) error {
	if err := remotes.Delete(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// POST /api/remotes/:id/test
// Returns { ok: true, version: "..." } or error.
func testRemote(c *fiber.Ctx) error {
	r, err := remotes.Get(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	if err := remoteclient.Ping(r.URL, r.Token); err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

// ALL /api/remotes/:id/proxy/*
// Forwards the request to the remote server with its stored Bearer token.
// The browser never sees the token — it only communicates with this server.
func proxyRemote(c *fiber.Ctx) error {
	r, err := remotes.Get(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "remote not found")
	}

	// Build target URL: remote base + /api/ + everything after /proxy
	// The local api.js strips the /api prefix from paths (e.g. "/tunnel-interfaces"),
	// so we must re-add /api/ when forwarding to the remote server.
	subPath := c.Params("*")
	if subPath == "" {
		subPath = "/"
	}
	if !strings.HasPrefix(subPath, "/") {
		subPath = "/" + subPath
	}
	targetURL := strings.TrimRight(r.URL, "/") + "/api" + subPath
	if qs := string(c.Request().URI().QueryString()); qs != "" {
		targetURL += "?" + qs
	}

	// Create outgoing request.
	req, err := http.NewRequest(c.Method(), targetURL, strings.NewReader(string(c.Body())))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "proxy build request: "+err.Error())
	}

	// Forward relevant headers (Content-Type, Accept, etc.) but inject our token.
	for k, vals := range c.GetReqHeaders() {
		lower := strings.ToLower(k)
		if lower == "authorization" || lower == "cookie" || lower == "host" {
			continue // don't forward auth/session headers from the browser
		}
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)

	resp, err := proxyClient.Do(req)
	if err != nil {
		log.Printf("[proxy] %s %s → remote error: %v", c.Method(), targetURL, err)
		return fiber.NewError(fiber.StatusBadGateway, "proxy request failed: "+err.Error())
	}
	defer resp.Body.Close()

	// Copy response status + safe headers + body.
	// IMPORTANT: never forward Set-Cookie from the remote — it would overwrite
	// the browser's local session cookie, causing immediate auth loss on the
	// local server. Also skip hop-by-hop headers that must not be forwarded.
	skipHeaders := map[string]bool{
		"Set-Cookie":          true,
		"Connection":          true,
		"Transfer-Encoding":   true,
		"Upgrade":             true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
		"Te":                  true,
		"Trailer":             true,
	}
	c.Status(resp.StatusCode)
	for k, vals := range resp.Header {
		if skipHeaders[k] {
			continue
		}
		for _, v := range vals {
			c.Set(k, v)
		}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "proxy read response: "+err.Error())
	}
	return c.Send(body)
}

// validateRemoteURL ensures the URL is a valid http/https address and rejects
// localhost / link-local targets to prevent SSRF.
func validateRemoteURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL must start with http:// or https://")
	}
	host := strings.ToLower(u.Hostname())
	blocked := []string{"localhost", "127.", "::1", "0.0.0.0", "169.254."}
	for _, b := range blocked {
		if strings.HasPrefix(host, b) || host == b {
			return fmt.Errorf("remote URL must not point to localhost or link-local addresses")
		}
	}
	return nil
}
