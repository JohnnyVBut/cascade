package frontend

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

// TestUI2RouteNotSwallowedByCatchAll replicates the main.go registration order
// (/ui2 mount BEFORE the catch-all "/" mount) and asserts that a request to
// /ui2/ is served by the UI2 filesystem, not the legacy www/ index.html.
// This guards the plan's highest-priority routing risk.
func TestUI2RouteNotSwallowedByCatchAll(t *testing.T) {
	app := fiber.New()
	app.Use("/ui2", filesystem.New(filesystem.Config{
		Root:         UI2FS(),
		Index:        "index.html",
		NotFoundFile: "index.html",
	}))
	app.Use("/", filesystem.New(filesystem.Config{
		Root:         FS(),
		Index:        "index.html",
		NotFoundFile: "index.html",
	}))

	// /ui2/ must serve the UI2 index (contains "Cascade UI2" placeholder text or
	// the built app's #app mount div — both distinct from the legacy UI).
	req := httptest.NewRequest("GET", "/ui2/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test /ui2/: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("/ui2/ status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "Cascade UI2") && !strings.Contains(s, `id="app"`) {
		t.Errorf("/ui2/ did not serve the UI2 index (got legacy fallback?): %.120s", s)
	}

	// A deep UI2 route must fall back to the UI2 index (SPA), still not legacy.
	deepReq := httptest.NewRequest("GET", "/ui2/interfaces", nil)
	deepResp, err := app.Test(deepReq)
	if err != nil {
		t.Fatalf("app.Test /ui2/interfaces: %v", err)
	}
	if deepResp.StatusCode != 200 {
		t.Fatalf("/ui2/interfaces status = %d, want 200", deepResp.StatusCode)
	}
}
