package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/gateway"
)

// RegisterDashboard registers /api/dashboard/* routes.
func RegisterDashboard(api fiber.Router) {
	g := api.Group("/dashboard")
	g.Get("/widgets", getDashboardWidgets)
	g.Put("/widgets", putDashboardWidgets)
	g.Get("/system-info", getSystemInfo)
}

// getDashboardWidgets returns the widget layout for the current user.
// Query param: ?page=dashboard (default) or ?page=diagnostics
// Response: { "widgets": [...] }
func getDashboardWidgets(c *fiber.Ctx) error {
	uid, ok := currentUserID(c)
	if !ok {
		return fiber.ErrUnauthorized
	}

	page := c.Query("page", "dashboard")
	if page != "dashboard" && page != "diagnostics" {
		return fiber.NewError(fiber.StatusBadRequest, "invalid page")
	}

	var widgetsJSON string
	row := db.DB().QueryRow(`SELECT widgets FROM dashboard_widgets WHERE user_id = ? AND page = ?`, uid, page)
	if err := row.Scan(&widgetsJSON); err != nil {
		widgetsJSON = "[]"
	}

	// Self-heal: DeleteGateway prunes "gateway:<id>" refs from saved widgets
	// going forward, but rows written before that fix (or ones that slip
	// through some other path) can still carry stale refs to gateways that
	// no longer exist. Cross-check against the live gateway list on every
	// read and persist the cleanup, so this converges to clean state on
	// its own without a one-off migration.
	//
	// This deliberately makes GET mutate stored state — a departure from
	// "GET should be safe" — but the mutation is idempotent, scoped to the
	// requesting user's own row, and best-effort (failures just skip
	// healing for this request, retried on the next load). Don't "fix" this
	// back to read-only without an alternative cleanup path in its place.
	//
	// The UPDATE is a compare-and-swap against the exact JSON just read
	// (`AND widgets = ?`), so a concurrent PUT saving a fresh layout for the
	// same user/page between our read and write makes this a no-op instead
	// of clobbering it.
	if cleaned, changed := pruneStaleGatewayRefsFromWidgetsJSON(widgetsJSON); changed {
		if _, err := db.DB().Exec(`UPDATE dashboard_widgets SET widgets = ? WHERE user_id = ? AND page = ? AND widgets = ?`,
			cleaned, uid, page, widgetsJSON); err != nil {
			log.Printf("dashboard: self-heal widgets: update failed for user %s/%s: %v", uid, page, err)
		}
		widgetsJSON = cleaned
	}

	c.Set("Content-Type", "application/json")
	return c.SendString(`{"widgets":` + widgetsJSON + `}`)
}

// pruneStaleGatewayRefsFromWidgetsJSON strips "gateway:<id>" entries from
// widgets' graphs/graphColors whose gateway no longer exists. Returns the
// (possibly unchanged) JSON and whether anything was removed. Malformed or
// empty input is returned as-is with changed=false — this is best-effort
// self-healing, not a source of truth.
func pruneStaleGatewayRefsFromWidgetsJSON(widgetsJSON string) (string, bool) {
	if widgetsJSON == "" || widgetsJSON == "[]" || widgetsJSON == "null" {
		return widgetsJSON, false
	}
	// Cheap short-circuit before paying for a gateway-table query + JSON
	// decode: nothing to prune if there's no "gateway:" ref in this row at
	// all (mirrors the LIKE prefilter in gateway.pruneDashboardWidgetsForGateway).
	if !strings.Contains(widgetsJSON, "gateway:") {
		return widgetsJSON, false
	}

	var widgets []map[string]interface{}
	if err := json.Unmarshal([]byte(widgetsJSON), &widgets); err != nil {
		return widgetsJSON, false
	}

	gws, err := gateway.Get().GetGateways()
	if err != nil {
		log.Printf("dashboard: self-heal widgets: fetch gateways failed: %v", err)
		return widgetsJSON, false
	}
	live := make(map[string]bool, len(gws))
	for _, gw := range gws {
		live["gateway:"+gw.ID] = true
	}

	changed := gateway.FilterGraphRefs(widgets, func(key string) bool {
		return strings.HasPrefix(key, "gateway:") && !live[key]
	})
	if !changed {
		return widgetsJSON, false
	}

	out, err := json.Marshal(widgets)
	if err != nil {
		log.Printf("dashboard: self-heal widgets: marshal failed: %v", err)
		return widgetsJSON, false
	}
	return string(out), true
}

// putDashboardWidgets saves the widget layout for the current user.
// Query param: ?page=dashboard (default) or ?page=diagnostics
// Body: { "widgets": [...] }
func putDashboardWidgets(c *fiber.Ctx) error {
	uid, ok := currentUserID(c)
	if !ok {
		return fiber.ErrUnauthorized
	}

	page := c.Query("page", "dashboard")
	if page != "dashboard" && page != "diagnostics" {
		return fiber.NewError(fiber.StatusBadRequest, "invalid page")
	}

	var payload struct {
		Widgets json.RawMessage `json:"widgets"`
	}
	if err := c.BodyParser(&payload); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}

	widgetsJSON := string(payload.Widgets)
	if widgetsJSON == "" || widgetsJSON == "null" {
		widgetsJSON = "[]"
	}

	if _, err := db.DB().Exec(`
		INSERT INTO dashboard_widgets (user_id, page, widgets)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, page) DO UPDATE SET widgets = excluded.widgets
	`, uid, page, widgetsJSON); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{"ok": true})
}

// ── System Info ───────────────────────────────────────────────────────────────

// SystemInfo holds server metrics for the dashboard system-info card.
type SystemInfo struct {
	Hostname  string  `json:"hostname"`
	Uptime    string  `json:"uptime"` // human-readable: "3d 4h 12m"
	UptimeSec int64   `json:"uptimeSec"`
	Load1     float64 `json:"load1"`
	Load5     float64 `json:"load5"`
	Load15    float64 `json:"load15"`
	MemTotal  int64   `json:"memTotal"` // kB
	MemFree   int64   `json:"memFree"`  // kB (MemAvailable)
	MemUsed   int64   `json:"memUsed"`  // kB
	MemPct    int     `json:"memPct"`   // 0-100
}

func getSystemInfo(c *fiber.Ctx) error {
	info := SystemInfo{}

	info.Hostname, _ = os.Hostname()

	// /proc/uptime: "12345.67 23456.78"
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		if parts := strings.Fields(string(data)); len(parts) >= 1 {
			if secs, err := strconv.ParseFloat(parts[0], 64); err == nil {
				info.UptimeSec = int64(secs)
				info.Uptime = formatUptime(int64(secs))
			}
		}
	}

	// /proc/loadavg: "0.12 0.34 0.56 1/234 5678"
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		if parts := strings.Fields(string(data)); len(parts) >= 3 {
			info.Load1, _ = strconv.ParseFloat(parts[0], 64)
			info.Load5, _ = strconv.ParseFloat(parts[1], 64)
			info.Load15, _ = strconv.ParseFloat(parts[2], 64)
		}
	}

	// /proc/meminfo
	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		memAvail := int64(-1)
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 2 {
				continue
			}
			val, _ := strconv.ParseInt(fields[1], 10, 64)
			switch fields[0] {
			case "MemTotal:":
				info.MemTotal = val
			case "MemFree:":
				info.MemFree = val
			case "MemAvailable:":
				memAvail = val
			}
		}
		if memAvail >= 0 {
			info.MemFree = memAvail
		}
		info.MemUsed = info.MemTotal - info.MemFree
		if info.MemTotal > 0 {
			info.MemPct = int(math.Round(float64(info.MemUsed) / float64(info.MemTotal) * 100))
		}
	}

	return c.JSON(info)
}

func formatUptime(secs int64) string {
	d := time.Duration(secs) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
