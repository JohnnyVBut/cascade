package api

import (
	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/version"
)

// RegisterVersion registers the unauthenticated GET /api/version endpoint.
// Must be called before the auth middleware so the UI can read the version
// and show the update banner before/without login.
func RegisterVersion(r fiber.Router) {
	r.Get("/version", getVersion)
	r.Post("/version/check", forceVersionCheck)
}

// forceVersionCheck triggers an immediate GitHub release check, bypassing the 24h cache.
// Returns the same shape as GET /api/version.
func forceVersionCheck(c *fiber.Ctx) error {
	version.Check()
	return getVersion(c)
}

// getVersion returns the running version and latest release info.
//
// Response:
//
//	{
//	  "version":         "v1.2.3",   // current running version ("dev" if built without ldflags)
//	  "gitCommit":       "abc1234",
//	  "latestVersion":   "v1.3.0",   // from GitHub Releases API (empty until first check)
//	  "releaseURL":      "https://github.com/...",
//	  "updateAvailable": true,
//	  "checkedAt":       "2026-03-28T12:00:00Z",
//	  "error":           ""          // non-empty if last check failed
//	}
func getVersion(c *fiber.Ctx) error {
	s := version.GetStatus()
	return c.JSON(fiber.Map{
		"version":         version.Version,
		"gitCommit":       version.GitCommit,
		"latestVersion":   s.LatestVersion,
		"releaseURL":      s.ReleaseURL,
		"updateAvailable": s.UpdateAvailable,
		"checkedAt":       s.CheckedAt,
		"error":           s.Error,
	})
}
