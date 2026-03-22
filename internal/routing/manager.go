// Package routing manages static routes and exposes kernel routing information.
// Port of RouteManager.js.
//
// Critical constraints (from CLAUDE.md):
//   - FIX-11: NEVER use "ip -j" — text parsing only (hangs on some kernels).
//   - FIX-13: RestoreAll() must be called AFTER InterfaceManager initialises all
//             WireGuard interfaces; otherwise "ip route add dev wgX" fails.
//   - FIX-15: Kernel errors from "ip route" are surfaced as HTTP 400 with
//             the exact stderr message (e.g. "RTNETLINK answers: Invalid argument").
//
// Persistence: SQLite `routes` table (see internal/db migration v3).
package routing

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/JohnnyVBut/awg-easy/internal/db"
	"github.com/JohnnyVBut/awg-easy/internal/util"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Route is a user-defined static route stored in SQLite.
type Route struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Destination string `json:"destination"` // CIDR or "default"
	Gateway     string `json:"gateway"`     // next-hop IP; empty if dev-only
	Dev         string `json:"dev"`         // interface name; empty if gateway-only
	Metric      *int   `json:"metric"`      // nil = no explicit metric
	Table       string `json:"table"`       // routing table name or number; default "main"
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"createdAt"`
}

// RoutingTable is a kernel routing table discovered via ip rule show + rt_tables.
type RoutingTable struct {
	ID   *int   `json:"id"`   // nil for the synthetic "all" entry
	Name string `json:"name"`
}

// KernelRoute is one route parsed from "ip route show" text output.
// FIX-11: never use "ip -j route show".
type KernelRoute struct {
	Dst      string `json:"dst"`
	Gateway  string `json:"gateway,omitempty"`
	Dev      string `json:"dev,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Metric   int    `json:"metric,omitempty"`
	Scope    string `json:"scope,omitempty"`
	PrefSrc  string `json:"prefsrc,omitempty"`
	Table    string `json:"table,omitempty"`
}

// RouteResult is the parsed output of "ip route get".
type RouteResult struct {
	Dst      string `json:"dst"`
	Gateway  string `json:"gateway,omitempty"`
	Dev      string `json:"dev,omitempty"`
	PrefSrc  string `json:"prefsrc,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Table    string `json:"table,omitempty"`
	Mark     string `json:"mark,omitempty"`
}

// Manager manages static routes. All state lives in SQLite.
type Manager struct{}

// New returns a Manager. db.Init() must have been called first.
func New() *Manager { return &Manager{} }

// ── Restore (FIX-13) ─────────────────────────────────────────────────────────

// RestoreAll applies all enabled static routes to the kernel.
//
// MUST be called after InterfaceManager has brought up all WireGuard interfaces
// (FIX-13). Errors are logged but not returned — a failed restore is non-fatal.
func (m *Manager) RestoreAll() {
	routes, err := m.GetRoutes()
	if err != nil {
		log.Printf("routing: restoreAll: getRoutes: %v", err)
		return
	}
	enabled := 0
	for _, r := range routes {
		if !r.Enabled {
			continue
		}
		enabled++
		if err := m.kernelAdd(&r); err != nil {
			// "File exists" = route already in kernel — normal after hot restart
			log.Printf("routing: restore %s: %v", r.Destination, err)
		} else {
			log.Printf("routing: restored %s", r.Destination)
		}
	}
	if enabled > 0 {
		log.Printf("routing: restoreAll done (%d routes)", enabled)
	}
}

// ReapplyForDevice re-adds all enabled routes that use the given interface.
//
// Called after TunnelInterface start/restart because wg-quick down→up removes
// all custom routes that use the interface from the kernel (FIX-13).
func (m *Manager) ReapplyForDevice(devName string) {
	routes, err := m.GetRoutes()
	if err != nil {
		log.Printf("routing: reapplyForDevice %s: %v", devName, err)
		return
	}
	for _, r := range routes {
		if !r.Enabled || r.Dev != devName {
			continue
		}
		if err := m.kernelAdd(&r); err != nil {
			log.Printf("routing: reapply %s via %s: %v", r.Destination, devName, err)
		} else {
			log.Printf("routing: reapplied %s via %s", r.Destination, devName)
		}
	}
}

// ── Kernel info (read-only) ───────────────────────────────────────────────────

// GetRoutingTables discovers routing tables via /etc/iproute2/rt_tables and
// "ip rule show" (text, no -j — FIX-11).
//
// Strategy:
//  1. Read /etc/iproute2/rt_tables → base id↔name mapping
//  2. Parse "ip rule show" → detect tables used in policy rules
//     (finds host tables like 100/vpn_kz via --network host)
//
// Returns tables sorted by id, with a synthetic {id:nil, name:"all"} appended.
func (m *Manager) GetRoutingTables() ([]RoutingTable, error) {
	skipIDs := map[int]bool{0: true, 255: true}
	skipNames := map[string]bool{"unspec": true, "local": true}

	// Step 1: read /etc/iproute2/rt_tables for base id↔name mapping.
	nameByID := map[int]string{}
	idByName := map[string]int{}
	if content, err := os.ReadFile("/etc/iproute2/rt_tables"); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			var id int
			if _, err := fmt.Sscanf(fields[0], "%d", &id); err != nil {
				continue
			}
			name := fields[1]
			nameByID[id] = name
			idByName[name] = id
		}
	} else {
		// Fallback defaults when rt_tables is not available.
		nameByID[253] = "default"
		nameByID[254] = "main"
		idByName["default"] = 253
		idByName["main"] = 254
	}

	// Step 2: discover tables via "ip rule show" (text, FIX-11).
	found := map[int]RoutingTable{} // id → table
	out, err := util.Exec("ip rule show", util.FastTimeout, false)
	if err != nil {
		// Fallback: use only rt_tables entries.
		for id, name := range nameByID {
			if skipIDs[id] || skipNames[name] {
				continue
			}
			found[id] = RoutingTable{ID: intPtr(id), Name: name}
		}
	} else {
		for _, line := range strings.Split(out, "\n") {
			// Match "lookup <token>" anywhere in the line.
			// Examples:
			//   "32766:  lookup main"
			//   "10000:  from all fwmark 0x3e8 lookup 100"
			idx := strings.Index(line, "lookup ")
			if idx == -1 {
				continue
			}
			token := strings.Fields(line[idx+7:])[0]
			var id int
			var name string
			// Try numeric id first.
			if _, err := fmt.Sscanf(token, "%d", &id); err == nil && fmt.Sprintf("%d", id) == token {
				name = nameByID[id]
				if name == "" {
					name = token
				}
			} else {
				// Named table.
				name = token
				n, ok := idByName[token]
				if !ok {
					continue
				}
				id = n
			}
			if skipIDs[id] || skipNames[name] {
				continue
			}
			if _, exists := found[id]; !exists {
				found[id] = RoutingTable{ID: intPtr(id), Name: name}
			}
		}
	}

	// Guarantee "main" (254) is always present.
	if _, ok := found[254]; !ok {
		name := nameByID[254]
		if name == "" {
			name = "main"
		}
		found[254] = RoutingTable{ID: intPtr(254), Name: name}
	}

	// Sort by ID.
	ids := make([]int, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	sortInts(ids)

	tables := make([]RoutingTable, 0, len(ids)+1)
	for _, id := range ids {
		tables = append(tables, found[id])
	}

	// Append synthetic "all" at the end.
	tables = append(tables, RoutingTable{ID: nil, Name: "all"})
	return tables, nil
}

// GetKernelRoutes returns routes from the kernel for the given table.
// Uses "ip route show table <table>" text output (FIX-11: never ip -j).
func (m *Manager) GetKernelRoutes(table string) ([]KernelRoute, error) {
	if table == "" {
		table = "main"
	}
	cmd := fmt.Sprintf("ip route show table %s", table)
	out, err := util.Exec(cmd, util.FastTimeout, true)
	if err != nil {
		msg := err.Error()
		// Table does not exist — return empty, not an error.
		if strings.Contains(msg, "Invalid argument") ||
			strings.Contains(msg, "No such process") ||
			strings.Contains(msg, "does not exist") ||
			strings.Contains(msg, "RTNETLINK") {
			return []KernelRoute{}, nil
		}
		return nil, fmt.Errorf("ip route error: %s", msg)
	}
	return parseTextRoutes(out), nil
}

// TestRoute runs "ip route get <ip> [mark <mark>]" and parses the result.
// Returns nil if the command produces no output.
//
// FIX-11: text output only, no -j.
// FIX-15: kernel errors returned as "ip route: <detail>".
// FIX-GO-8: "ip route get <dst> from <src>" is NOT used — when src is a
// non-local address the kernel returns "RTNETLINK: Network unreachable".
// PBR simulation is done via firewall.SimulateTrace → fwmark → mark flag.
func (m *Manager) TestRoute(ip string, mark *int) (*RouteResult, error) {
	var cmd string
	if mark != nil {
		cmd = fmt.Sprintf("ip route get %s mark %d", ip, *mark)
	} else {
		cmd = fmt.Sprintf("ip route get %s", ip)
	}

	out, err := util.Exec(cmd, util.FastTimeout, true)
	if err != nil {
		detail := err.Error()
		if execErr, ok := err.(*util.ExecError); ok && execErr.Stderr != "" {
			detail = strings.TrimSpace(execErr.Stderr)
		}
		return nil, fmt.Errorf("ip route: %s", detail)
	}
	if out == "" {
		return nil, nil
	}

	// ip route get returns one line (or several for nexthop); take the first non-empty.
	firstLine := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			firstLine = strings.TrimSpace(l)
			break
		}
	}
	if firstLine == "" {
		return nil, nil
	}

	tokens := strings.Fields(firstLine)
	result := &RouteResult{Dst: tokens[0]}
	for i := 1; i < len(tokens); i++ {
		k := tokens[i]
		if i+1 >= len(tokens) {
			break
		}
		v := tokens[i+1]
		switch k {
		case "via":
			result.Gateway = v
			i++
		case "dev":
			result.Dev = v
			i++
		case "src", "from":
			// "ip route get X from Y" returns "from Y" instead of "src Y"
			result.PrefSrc = v
			i++
		case "proto":
			result.Protocol = v
			i++
		case "table":
			result.Table = v
			i++
		case "mark":
			result.Mark = v
			i++
		}
	}
	return result, nil
}

// ── Static route CRUD ─────────────────────────────────────────────────────────

// GetRoutes returns all static routes ordered by created_at ASC.
func (m *Manager) GetRoutes() ([]Route, error) {
	rows, err := db.DB().Query(`
		SELECT id, description, destination, via, dev, metric, table_name, enabled, created_at
		FROM routes ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("routes query: %w", err)
	}
	defer rows.Close()

	var out []Route
	for rows.Next() {
		r, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, nil
}

// GetRoute returns a single route by id, or nil if not found.
func (m *Manager) GetRoute(id string) (*Route, error) {
	return queryRoute(`WHERE id = ?`, id)
}

// AddRoute creates a new static route and applies it to the kernel immediately.
// Returns "ip route: <detail>" error on kernel failure (FIX-15 → HTTP 400).
func (m *Manager) AddRoute(data Route) (*Route, error) {
	if data.Destination == "" {
		return nil, fmt.Errorf("destination is required")
	}
	if data.Gateway == "" && data.Dev == "" {
		return nil, fmt.Errorf("gateway or interface is required")
	}
	if data.Table == "" {
		data.Table = "main"
	}

	r := Route{
		ID:          uuid.NewString(),
		Description: data.Description,
		Destination: data.Destination,
		Gateway:     data.Gateway,
		Dev:         data.Dev,
		Metric:      data.Metric,
		Table:       data.Table,
		Enabled:     true,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	// Apply to kernel first — fail fast before persisting.
	if err := m.kernelAdd(&r); err != nil {
		return nil, err
	}

	if err := insertRoute(&r); err != nil {
		// Best-effort rollback — if persist fails, remove from kernel.
		m.kernelDel(&r) //nolint:errcheck
		return nil, err
	}

	log.Printf("routing: added %s", r.Destination)
	return &r, nil
}

// DeleteRoute removes a static route from the kernel and from SQLite.
func (m *Manager) DeleteRoute(id string) error {
	r, err := m.getOrNotFound(id)
	if err != nil {
		return err
	}

	if r.Enabled {
		if err := m.kernelDel(r); err != nil {
			// Route may have been removed externally (e.g. wg-quick down) — non-fatal.
			log.Printf("routing: kernelDel %s on delete: %v", r.Destination, err)
		}
	}

	if _, err := db.DB().Exec(`DELETE FROM routes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete route: %w", err)
	}

	log.Printf("routing: deleted %s", r.Destination)
	return nil
}

// ToggleRoute enables or disables a route and syncs the kernel state accordingly.
// Returns "ip route: <detail>" error on kernel failure when enabling (FIX-15).
func (m *Manager) ToggleRoute(id string, enabled bool) (*Route, error) {
	r, err := m.getOrNotFound(id)
	if err != nil {
		return nil, err
	}

	if enabled && !r.Enabled {
		if err := m.kernelAdd(r); err != nil {
			return nil, err // already formatted as "ip route: ..."
		}
	} else if !enabled && r.Enabled {
		if err := m.kernelDel(r); err != nil {
			// Route may already be gone (wg-quick down etc.) — log but don't fail.
			log.Printf("routing: kernelDel %s on disable: %v", r.Destination, err)
		}
	}

	r.Enabled = enabled
	if _, err := db.DB().Exec(`UPDATE routes SET enabled = ? WHERE id = ?`,
		boolInt(enabled), id); err != nil {
		return nil, fmt.Errorf("toggle route: %w", err)
	}

	return r, nil
}

// UpdateRoute applies partial updates (description, destination, gateway, dev,
// metric, table) to a route. Disabled routes are only updated in SQLite.
// Enabled routes are re-applied to the kernel (del old → add new).
func (m *Manager) UpdateRoute(id string, data Route) (*Route, error) {
	r, err := m.getOrNotFound(id)
	if err != nil {
		return nil, err
	}

	// Save old values for kernel rollback.
	old := *r

	if data.Description != "" {
		r.Description = data.Description
	}
	if data.Destination != "" {
		r.Destination = data.Destination
	}
	if data.Gateway != "" || data.Dev != "" {
		r.Gateway = data.Gateway
		r.Dev = data.Dev
	}
	r.Metric = data.Metric
	if data.Table != "" {
		r.Table = data.Table
	}

	if r.Enabled {
		// Remove old route from kernel, add new one.
		m.kernelDel(&old) //nolint:errcheck
		if err := m.kernelAdd(r); err != nil {
			// Rollback: restore old route.
			m.kernelAdd(&old) //nolint:errcheck
			return nil, err
		}
	}

	if err := updateRoute(r); err != nil {
		return nil, err
	}

	return r, nil
}

// ── Kernel helpers ────────────────────────────────────────────────────────────

// kernelAdd runs "ip route add ..." and wraps stderr as "ip route: <detail>" (FIX-15).
func (m *Manager) kernelAdd(r *Route) error {
	_, err := util.ExecDefault(buildAddCmd(r))
	if err != nil {
		return wrapKernelErr(err)
	}
	return nil
}

// kernelDel runs "ip route del ...". Errors are returned as-is (non-fatal callers).
func (m *Manager) kernelDel(r *Route) error {
	_, err := util.ExecDefault(buildDelCmd(r))
	return err
}

func buildAddCmd(r *Route) string {
	cmd := "ip route add " + r.Destination
	if r.Gateway != "" {
		cmd += " via " + r.Gateway
	}
	if r.Dev != "" {
		cmd += " dev " + r.Dev
	}
	// proto static — marks the route as user-defined (admin-added).
	// Without this the kernel uses proto "boot" which shows as "--" in
	// "ip route show" and in the kernel routes UI table.
	cmd += " proto static"
	if r.Metric != nil {
		cmd += fmt.Sprintf(" metric %d", *r.Metric)
	}
	if r.Table != "" && r.Table != "main" {
		cmd += " table " + r.Table
	}
	return cmd
}

func buildDelCmd(r *Route) string {
	cmd := "ip route del " + r.Destination
	if r.Gateway != "" {
		cmd += " via " + r.Gateway
	}
	if r.Dev != "" {
		cmd += " dev " + r.Dev
	}
	if r.Table != "" && r.Table != "main" {
		cmd += " table " + r.Table
	}
	return cmd
}

// wrapKernelErr formats a util.ExecError as "ip route: <stderr>" (FIX-15).
// HTTP handlers check strings.HasPrefix(err.Error(), "ip route:") → 400.
func wrapKernelErr(err error) error {
	if execErr, ok := err.(*util.ExecError); ok && strings.TrimSpace(execErr.Stderr) != "" {
		return fmt.Errorf("ip route: %s", strings.TrimSpace(execErr.Stderr))
	}
	return fmt.Errorf("ip route: %s", err.Error())
}

// ── Text parsers (FIX-11) ─────────────────────────────────────────────────────

// parseTextRoutes parses "ip route show" text output into KernelRoute structs.
// FIX-11: text output only — "ip -j route show" hangs on some kernels.
//
// Line format: <dst> [via <gw>] dev <dev> proto <proto> [scope <scope>] [src <src>] [metric <n>]
// Example:
//
//	default via 62.113.116.1 dev ens3 proto static onlink
//	10.8.0.0/24 dev wg0 proto kernel scope link src 10.8.0.1
func parseTextRoutes(text string) []KernelRoute {
	var routes []KernelRoute
	for _, rawLine := range strings.Split(text, "\n") {
		// Lines starting with whitespace are nexthop continuations — skip.
		if len(rawLine) > 0 && (rawLine[0] == '\t' || rawLine[0] == ' ') {
			continue
		}
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		tokens := strings.Fields(line)
		if len(tokens) < 2 {
			continue
		}

		r := KernelRoute{Dst: tokens[0]}
		for i := 1; i < len(tokens); i++ {
			k := tokens[i]
			if i+1 >= len(tokens) {
				break
			}
			v := tokens[i+1]
			switch k {
			case "via":
				r.Gateway = v
				i++
			case "dev":
				r.Dev = v
				i++
			case "proto":
				r.Protocol = v
				i++
			case "metric":
				fmt.Sscanf(v, "%d", &r.Metric)
				i++
			case "scope":
				r.Scope = v
				i++
			case "src":
				r.PrefSrc = v
				i++
			case "table":
				r.Table = v
				i++
			}
		}
		routes = append(routes, r)
	}
	return routes
}

// ── DB helpers ────────────────────────────────────────────────────────────────

func queryRoute(where string, args ...any) (*Route, error) {
	row := db.DB().QueryRow(`
		SELECT id, description, destination, via, dev, metric, table_name, enabled, created_at
		FROM routes `+where, args...)
	return scanRoute(row)
}

type scannable interface {
	Scan(dest ...any) error
}

func scanRoute(s scannable) (*Route, error) {
	var (
		metric  sql.NullInt64
		enabled int
	)
	var r Route
	err := s.Scan(
		&r.ID, &r.Description, &r.Destination, &r.Gateway, &r.Dev,
		&metric, &r.Table, &enabled, &r.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("route scan: %w", err)
	}
	if metric.Valid {
		n := int(metric.Int64)
		r.Metric = &n
	}
	r.Enabled = enabled == 1
	return &r, nil
}

func insertRoute(r *Route) error {
	_, err := db.DB().Exec(`
		INSERT INTO routes (id, description, destination, via, dev, metric, table_name, enabled, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Description, r.Destination, r.Gateway, r.Dev,
		metricVal(r.Metric), r.Table, boolInt(r.Enabled), r.CreatedAt,
	)
	return err
}

func updateRoute(r *Route) error {
	_, err := db.DB().Exec(`
		UPDATE routes SET
		    description=?, destination=?, via=?, dev=?, metric=?, table_name=?
		WHERE id=?`,
		r.Description, r.Destination, r.Gateway, r.Dev,
		metricVal(r.Metric), r.Table, r.ID,
	)
	return err
}

func (m *Manager) getOrNotFound(id string) (*Route, error) {
	r, err := m.GetRoute(id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("route not found")
	}
	return r, nil
}

// ── Misc helpers ──────────────────────────────────────────────────────────────

func metricVal(m *int) any {
	if m == nil {
		return nil
	}
	return *m
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intPtr(n int) *int { return &n }

// sortInts sorts an int slice in-place (avoids importing sort for a trivial use).
func sortInts(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ── Singleton accessor ────────────────────────────────────────────────────────

var instance *Manager

// SetInstance stores the initialized Manager for package-level access.
// Must be called from main() before serving requests.
func SetInstance(m *Manager) { instance = m }

// Get returns the package-level Manager singleton.
// Panics with a clear message if SetInstance was not called (programming error).
func Get() *Manager {
	if instance == nil {
		panic("routing: manager not initialized — call SetInstance before Get()")
	}
	return instance
}
