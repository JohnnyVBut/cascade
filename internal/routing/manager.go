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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/gateway"
	"github.com/JohnnyVBut/cascade/internal/util"
	"github.com/JohnnyVBut/cascade/internal/validate"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Route is a user-defined static route stored in SQLite.
type Route struct {
	ID             string `json:"id"`
	Description    string `json:"description"`
	Destination    string `json:"destination"`    // CIDR or "default"
	Gateway        string `json:"gateway"`         // manual next-hop IP; empty if using GatewayID
	Dev            string `json:"dev"`             // interface name; empty if gateway-only
	Metric         *int   `json:"metric"`          // nil = no explicit metric
	Table          string `json:"table"`           // routing table name or number; default "main"
	Enabled        bool   `json:"enabled"`
	GatewayID      string `json:"gatewayId"`       // linked Gateway — resolves via/dev dynamically
	GatewayGroupID string `json:"gatewayGroupId"`  // linked GatewayGroup — failover between tiers
	CreatedAt      string `json:"createdAt"`
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
// Gateway-aware routes (GatewayID / GatewayGroupID) are resolved at runtime:
// the via/dev are not stored in the DB but derived from the gateway's current IP/interface.
// When a gateway status changes, affected routes are updated via "ip route replace".
type Manager struct {
	gwMgr         *gateway.Manager // nil until SubscribeToMonitor is called
	mu            sync.Mutex
	fallbackActive map[string]bool         // routeID → true when not on primary gateway
	restoreTimers  map[string]*time.Timer  // routeID → pending primary-restore timer
}

// New returns a Manager. db.Init() must have been called first.
func New() *Manager {
	return &Manager{
		fallbackActive: make(map[string]bool),
		restoreTimers:  make(map[string]*time.Timer),
	}
}

// ── Gateway-aware failover ─────────────────────────────────────────────────────

// SubscribeToMonitor registers gateway status callbacks so that routes with
// GatewayID / GatewayGroupID are automatically updated when a gateway goes
// down or recovers. Must be called before RestoreAll().
func (m *Manager) SubscribeToMonitor(gwMgr *gateway.Manager) {
	m.gwMgr = gwMgr
	gwMgr.Monitor().OnStatusChange(func(gatewayID, newStatus, prevStatus string) {
		m.handleGatewayStatusChange(gatewayID, newStatus, prevStatus)
	})
	log.Printf("routing: subscribed to gateway monitor")
}

// handleGatewayStatusChange is the GatewayMonitor callback.
// On DOWN: immediately switch affected routes to the next available gateway.
// On recovery (UP): schedule restore to primary after 30 s (anti-flap).
func (m *Manager) handleGatewayStatusChange(gatewayID, newStatus, prevStatus string) {
	if newStatus == prevStatus {
		return
	}
	routes, err := m.GetRoutes()
	if err != nil {
		log.Printf("routing: handleGatewayStatusChange: GetRoutes: %v", err)
		return
	}

	isDown := func(s string) bool { return s == "down" || s == "admin_down" }
	goingDown := isDown(newStatus) && !isDown(prevStatus)
	recovering := !isDown(newStatus) && isDown(prevStatus)

	for i := range routes {
		r := &routes[i]
		if !r.Enabled || !m.routeReferencesGateway(r, gatewayID) {
			continue
		}

		if goingDown {
			m.mu.Lock()
			// Cancel any pending restore timer.
			if t, ok := m.restoreTimers[r.ID]; ok {
				t.Stop()
				delete(m.restoreTimers, r.ID)
			}
			m.mu.Unlock()

			// Re-resolve the best available gateway (may switch to backup tier).
			via, dev, err := m.resolveGatewayVia(r)
			if err != nil {
				log.Printf("routing: gateway %s down, cannot re-resolve route %s: %v",
					gatewayID, r.Destination, err)
				continue
			}
			if err := m.kernelReplace(r, via, dev); err != nil {
				log.Printf("routing: gateway %s down, replace route %s: %v",
					gatewayID, r.Destination, err)
				continue
			}
			// Determine if we switched away from primary.
			primaryVia, primaryDev, err := m.primaryGatewayVia(r)
			inFallback := err != nil || via != primaryVia || dev != primaryDev
			m.mu.Lock()
			m.fallbackActive[r.ID] = inFallback
			m.mu.Unlock()
			if inFallback {
				log.Printf("routing: route %s → fallback via %s dev %s (gateway %s down)",
					r.Destination, via, dev, gatewayID)
			}

		} else if recovering {
			m.mu.Lock()
			inFallback := m.fallbackActive[r.ID]
			m.mu.Unlock()
			if !inFallback {
				continue
			}

			// Check if this recovery event restores the primary tier.
			primaryVia, primaryDev, err := m.primaryGatewayVia(r)
			if err != nil {
				continue
			}
			routeCopy := *r
			log.Printf("routing: route %s: gateway %s recovered, restore primary in 30s",
				r.Destination, gatewayID)
			m.mu.Lock()
			if t, ok := m.restoreTimers[r.ID]; ok {
				t.Stop()
			}
			m.restoreTimers[r.ID] = time.AfterFunc(30*time.Second, func() {
				m.mu.Lock()
				delete(m.restoreTimers, routeCopy.ID)
				m.mu.Unlock()
				if err := m.kernelReplace(&routeCopy, primaryVia, primaryDev); err != nil {
					log.Printf("routing: restore route %s: %v", routeCopy.Destination, err)
					return
				}
				m.mu.Lock()
				m.fallbackActive[routeCopy.ID] = false
				m.mu.Unlock()
				log.Printf("routing: route %s → restored primary via %s dev %s",
					routeCopy.Destination, primaryVia, primaryDev)
			})
			m.mu.Unlock()
		}
	}
}

// routeReferencesGateway returns true if the route is directly linked to
// gatewayID or if gatewayID is a member of the route's gateway group.
func (m *Manager) routeReferencesGateway(r *Route, gatewayID string) bool {
	if r.GatewayID == gatewayID {
		return true
	}
	if r.GatewayGroupID != "" && m.gwMgr != nil {
		grp, err := m.gwMgr.GetGroup(r.GatewayGroupID)
		if err != nil || grp == nil {
			return false
		}
		for _, mem := range grp.Gateways {
			if mem.GatewayID == gatewayID {
				return true
			}
		}
	}
	return false
}

// resolveGatewayVia picks the best available via/dev for a gateway-aware route.
// For GatewayID: returns that gateway's IP + interface.
// For GatewayGroupID: picks the lowest-tier gateway that is not "down".
//   Falls back to the lowest-tier gateway if all are down (gateway of last resort).
// For plain routes (no gateway reference): returns r.Gateway, r.Dev.
func (m *Manager) resolveGatewayVia(r *Route) (via, dev string, err error) {
	if r.GatewayID != "" && m.gwMgr != nil {
		gw, err := m.gwMgr.GetGateway(r.GatewayID)
		if err != nil || gw == nil {
			return "", "", fmt.Errorf("gateway %s not found", r.GatewayID)
		}
		return gw.GatewayIP, gw.Interface, nil
	}
	if r.GatewayGroupID != "" && m.gwMgr != nil {
		return m.resolveGroupGateway(r.GatewayGroupID)
	}
	return r.Gateway, r.Dev, nil
}

// primaryGatewayVia returns the via/dev for the highest-priority (tier1) gateway.
// Used to determine if a route is currently on its primary gateway.
func (m *Manager) primaryGatewayVia(r *Route) (via, dev string, err error) {
	if r.GatewayID != "" && m.gwMgr != nil {
		gw, err := m.gwMgr.GetGateway(r.GatewayID)
		if err != nil || gw == nil {
			return "", "", fmt.Errorf("gateway %s not found", r.GatewayID)
		}
		return gw.GatewayIP, gw.Interface, nil
	}
	if r.GatewayGroupID != "" && m.gwMgr != nil {
		grp, err := m.gwMgr.GetGroup(r.GatewayGroupID)
		if err != nil || grp == nil || len(grp.Gateways) == 0 {
			return "", "", fmt.Errorf("gateway group %s not found or empty", r.GatewayGroupID)
		}
		// Find the member with the lowest tier number.
		best := grp.Gateways[0]
		for _, mem := range grp.Gateways[1:] {
			if mem.Tier < best.Tier {
				best = mem
			}
		}
		gw, err := m.gwMgr.GetGateway(best.GatewayID)
		if err != nil || gw == nil {
			return "", "", fmt.Errorf("primary gateway %s not found", best.GatewayID)
		}
		return gw.GatewayIP, gw.Interface, nil
	}
	return "", "", fmt.Errorf("route has no gateway reference")
}

// resolveGroupGateway picks the best available gateway from a gateway group.
// Iterates tiers from lowest (highest priority) upward.
// Within a tier picks the first healthy/degraded/unknown member.
// Falls back to the tier1 gateway if all members are "down".
func (m *Manager) resolveGroupGateway(groupID string) (via, dev string, err error) {
	grp, err := m.gwMgr.GetGroup(groupID)
	if err != nil || grp == nil || len(grp.Gateways) == 0 {
		return "", "", fmt.Errorf("gateway group %s not found or empty", groupID)
	}

	mon := m.gwMgr.Monitor()

	// Sort members by tier ascending (lowest = highest priority).
	sorted := make([]gateway.GatewayGroupMember, len(grp.Gateways))
	copy(sorted, grp.Gateways)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Tier < sorted[j].Tier })

	// Group members by tier.
	type tierEntry struct {
		tier    int
		members []gateway.GatewayGroupMember
	}
	var tiers []tierEntry
	for _, mem := range sorted {
		if len(tiers) == 0 || tiers[len(tiers)-1].tier != mem.Tier {
			tiers = append(tiers, tierEntry{tier: mem.Tier})
		}
		tiers[len(tiers)-1].members = append(tiers[len(tiers)-1].members, mem)
	}

	var fallbackGW *gateway.Gateway // tier1 gateway, used if all tiers down

	for _, te := range tiers {
		for _, mem := range te.members {
			gw, err := m.gwMgr.GetGateway(mem.GatewayID)
			if err != nil || gw == nil {
				continue
			}
			if fallbackGW == nil {
				fallbackGW = gw // remember tier1 as last resort
			}
			st := mon.GetStatus(mem.GatewayID)
			// Use this gateway unless it is explicitly "down" or "admin_down".
			// "unknown" = not enough probes yet → treat as available.
			if st.Status != "down" && st.Status != "admin_down" {
				return gw.GatewayIP, gw.Interface, nil
			}
		}
	}

	// All gateways are "down" — route via tier1 as gateway of last resort.
	if fallbackGW != nil {
		return fallbackGW.GatewayIP, fallbackGW.Interface, nil
	}
	return "", "", fmt.Errorf("no valid gateway found in group %s", groupID)
}

// kernelReplace runs "ip route replace ..." — idempotent unlike "ip route add".
func (m *Manager) kernelReplace(r *Route, via, dev string) error {
	cmd := "ip route replace " + r.Destination
	if via != "" {
		cmd += " via " + via
	}
	if dev != "" {
		cmd += " dev " + dev
	}
	cmd += " proto static"
	if r.Metric != nil {
		cmd += fmt.Sprintf(" metric %d", *r.Metric)
	}
	if r.Table != "" && r.Table != "main" {
		cmd += " table " + r.Table
	}
	_, err := util.ExecDefault(cmd)
	if err != nil {
		return wrapKernelErr(err)
	}
	return nil
}

// ── Restore (FIX-13) ─────────────────────────────────────────────────────────

// RestoreAll applies all enabled static routes to the kernel.
//
// MUST be called after InterfaceManager has brought up all WireGuard interfaces
// (FIX-13). Errors are logged but not returned — a failed restore is non-fatal.
// Gateway-aware routes (GatewayID/GatewayGroupID) resolve via/dev at call time.
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
		// Use replace (not add) so that AllowedIPs routes added by wg-quick with
		// proto=kernel do not cause EEXIST when the user has a static route for
		// the same destination. "ip route replace" atomically overwrites the
		// existing route (if any) with proto=static, or adds it if absent.
		if err := m.kernelReplaceResolved(&r); err != nil {
			log.Printf("routing: restore %s: %v", r.Destination, err)
		} else {
			log.Printf("routing: restored %s", r.Destination)
		}
	}
	if enabled > 0 {
		log.Printf("routing: restoreAll done (%d routes)", enabled)
	}
}

// ReapplyGatewayGroup re-resolves and re-applies all enabled static routes
// that reference groupID. Called after gateway group tier configuration changes
// so the kernel routes reflect the new priority ordering immediately.
func (m *Manager) ReapplyGatewayGroup(groupID string) {
	routes, err := m.GetRoutes()
	if err != nil {
		log.Printf("routing: reapplyGatewayGroup %s: %v", groupID, err)
		return
	}
	for _, r := range routes {
		if !r.Enabled || r.GatewayGroupID != groupID {
			continue
		}
		if err := m.kernelReplaceResolved(&r); err != nil {
			log.Printf("routing: reapplyGatewayGroup %s route %s: %v", groupID, r.Destination, err)
		} else {
			log.Printf("routing: reapplied route %s for group %s", r.Destination, groupID)
		}
	}
}

// ReapplyForDevice re-adds all enabled routes that use the given interface.
//
// Called after TunnelInterface start/restart because wg-quick down→up removes
// all custom routes that use the interface from the kernel (FIX-13).
// For gateway-aware routes, re-resolves the current best via/dev.
func (m *Manager) ReapplyForDevice(devName string) {
	routes, err := m.GetRoutes()
	if err != nil {
		log.Printf("routing: reapplyForDevice %s: %v", devName, err)
		return
	}
	for _, r := range routes {
		if !r.Enabled {
			continue
		}
		// For gateway-aware routes, check if the resolved dev matches.
		if r.GatewayID != "" || r.GatewayGroupID != "" {
			via, dev, err := m.resolveGatewayVia(&r)
			if err != nil || dev != devName {
				continue
			}
			if err := m.kernelReplace(&r, via, dev); err != nil {
				log.Printf("routing: reapply gw-route %s via %s: %v", r.Destination, devName, err)
			} else {
				log.Printf("routing: reapplied gw-route %s via %s", r.Destination, devName)
			}
			continue
		}
		if r.Dev != devName {
			continue
		}
		// Use replace for the same reason as RestoreAll — wg-quick up may have
		// added an AllowedIPs route (proto kernel) that conflicts with our route.
		if err := m.kernelReplace(&r, r.Gateway, r.Dev); err != nil {
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
	if err := validate.TableName(table); err != nil {
		return nil, err
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
	if err := validate.IP(ip); err != nil {
		return nil, err
	}
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
		SELECT id, description, destination, via, dev, metric, table_name, enabled,
		       gateway_id, gateway_group_id, created_at
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
	// Require either a manual next-hop or a gateway reference.
	hasManual := data.Gateway != "" || data.Dev != ""
	hasGatewayRef := data.GatewayID != "" || data.GatewayGroupID != ""
	if !hasManual && !hasGatewayRef {
		return nil, fmt.Errorf("gateway, interface, gatewayId, or gatewayGroupId is required")
	}
	if data.GatewayID != "" && data.GatewayGroupID != "" {
		return nil, fmt.Errorf("gatewayId and gatewayGroupId are mutually exclusive")
	}
	if data.Table == "" {
		data.Table = "main"
	}
	if err := validateRouteFields(data); err != nil {
		return nil, err
	}

	r := Route{
		ID:             uuid.NewString(),
		Description:    data.Description,
		Destination:    data.Destination,
		Gateway:        data.Gateway,
		Dev:            data.Dev,
		Metric:         data.Metric,
		Table:          data.Table,
		Enabled:        true,
		GatewayID:      data.GatewayID,
		GatewayGroupID: data.GatewayGroupID,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}

	// Apply to kernel first — fail fast before persisting.
	if err := m.kernelAddResolved(&r); err != nil {
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

	// Clear failover state.
	m.mu.Lock()
	delete(m.fallbackActive, r.ID)
	if t, ok := m.restoreTimers[r.ID]; ok {
		t.Stop()
		delete(m.restoreTimers, r.ID)
	}
	m.mu.Unlock()

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
		if err := m.kernelAddResolved(r); err != nil {
			return nil, err // already formatted as "ip route: ..."
		}
	} else if !enabled && r.Enabled {
		if err := m.kernelDel(r); err != nil {
			// Route may already be gone (wg-quick down etc.) — log but don't fail.
			log.Printf("routing: kernelDel %s on disable: %v", r.Destination, err)
		}
		// Clear any failover state when explicitly disabling.
		m.mu.Lock()
		delete(m.fallbackActive, r.ID)
		if t, ok := m.restoreTimers[r.ID]; ok {
			t.Stop()
			delete(m.restoreTimers, r.ID)
		}
		m.mu.Unlock()
	}

	r.Enabled = enabled
	if _, err := db.DB().Exec(`UPDATE routes SET enabled = ? WHERE id = ?`,
		boolInt(enabled), id); err != nil {
		return nil, fmt.Errorf("toggle route: %w", err)
	}

	return r, nil
}

// UpdateRoute applies partial updates (description, destination, gateway, dev,
// metric, table, gatewayId, gatewayGroupId) to a route.
// Disabled routes are only updated in SQLite.
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
	// When gateway reference is being set, clear manual via/dev and vice-versa.
	hasGatewayRef := data.GatewayID != "" || data.GatewayGroupID != ""
	hasManual := data.Gateway != "" || data.Dev != ""
	if hasGatewayRef {
		r.GatewayID = data.GatewayID
		r.GatewayGroupID = data.GatewayGroupID
		r.Gateway = ""
		r.Dev = ""
	} else if hasManual {
		r.Gateway = data.Gateway
		r.Dev = data.Dev
		r.GatewayID = ""
		r.GatewayGroupID = ""
	}
	r.Metric = data.Metric
	if data.Table != "" {
		r.Table = data.Table
	}
	if err := validateRouteFields(*r); err != nil {
		return nil, err
	}

	if r.Enabled {
		// Remove old route from kernel, add new one.
		m.kernelDel(&old) //nolint:errcheck
		if err := m.kernelAddResolved(r); err != nil {
			// Rollback: restore old route.
			m.kernelAddResolved(&old) //nolint:errcheck
			return nil, err
		}
	}

	// Clear failover state — user updated the route explicitly.
	m.mu.Lock()
	delete(m.fallbackActive, r.ID)
	if t, ok := m.restoreTimers[r.ID]; ok {
		t.Stop()
		delete(m.restoreTimers, r.ID)
	}
	m.mu.Unlock()

	if err := updateRoute(r); err != nil {
		return nil, err
	}

	return r, nil
}

// ── Validation ────────────────────────────────────────────────────────────────

// validateRouteFields checks all user-supplied Route fields before they are
// interpolated into shell commands (CRIT-1 fix: command injection prevention).
func validateRouteFields(r Route) error {
	// Destination: CIDR or the literal "default".
	if r.Destination != "" && r.Destination != "default" {
		if err := validate.CIDR(r.Destination); err != nil {
			return fmt.Errorf("invalid destination: %w", err)
		}
	}
	// Gateway: must be a valid IP if provided.
	if r.Gateway != "" {
		if err := validate.IP(r.Gateway); err != nil {
			return fmt.Errorf("invalid gateway: %w", err)
		}
	}
	// Dev: must be a safe Linux interface name if provided.
	if r.Dev != "" {
		if err := validate.IfaceName(r.Dev); err != nil {
			return fmt.Errorf("invalid interface: %w", err)
		}
	}
	// Table: must be a safe iproute2 table name or number.
	if r.Table != "" {
		if err := validate.TableName(r.Table); err != nil {
			return fmt.Errorf("invalid table: %w", err)
		}
	}
	return nil
}

// ── Kernel helpers ────────────────────────────────────────────────────────────

// kernelAddResolved resolves gateway reference (if any) then runs "ip route add".
// This is the preferred call site — use kernelAdd only when via/dev are already known.
func (m *Manager) kernelAddResolved(r *Route) error {
	via, dev, err := m.resolveGatewayVia(r)
	if err != nil {
		return fmt.Errorf("ip route: cannot resolve gateway: %w", err)
	}
	resolved := *r
	resolved.Gateway = via
	resolved.Dev = dev
	return m.kernelAdd(&resolved)
}

// kernelReplaceResolved resolves gateway reference (if any) then runs "ip route replace".
// Unlike kernelAddResolved (which uses "ip route add" and fails if a route to the
// same destination already exists), replace is idempotent:
//   - Route absent → added.
//   - Route present (any proto) → replaced with proto static.
//
// This is the correct call site for RestoreAll and ReapplyForDevice, where
// wg-quick may have already added AllowedIPs routes (proto kernel) to the same
// destination — "ip route add" would fail with EEXIST in that case.
func (m *Manager) kernelReplaceResolved(r *Route) error {
	via, dev, err := m.resolveGatewayVia(r)
	if err != nil {
		return fmt.Errorf("ip route: cannot resolve gateway: %w", err)
	}
	return m.kernelReplace(r, via, dev)
}

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
		SELECT id, description, destination, via, dev, metric, table_name, enabled,
		       gateway_id, gateway_group_id, created_at
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
		&metric, &r.Table, &enabled,
		&r.GatewayID, &r.GatewayGroupID,
		&r.CreatedAt,
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
		INSERT INTO routes
		    (id, description, destination, via, dev, metric, table_name, enabled,
		     gateway_id, gateway_group_id, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Description, r.Destination, r.Gateway, r.Dev,
		metricVal(r.Metric), r.Table, boolInt(r.Enabled),
		r.GatewayID, r.GatewayGroupID, r.CreatedAt,
	)
	return err
}

func updateRoute(r *Route) error {
	_, err := db.DB().Exec(`
		UPDATE routes SET
		    description=?, destination=?, via=?, dev=?, metric=?, table_name=?,
		    gateway_id=?, gateway_group_id=?
		WHERE id=?`,
		r.Description, r.Destination, r.Gateway, r.Dev,
		metricVal(r.Metric), r.Table,
		r.GatewayID, r.GatewayGroupID,
		r.ID,
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
