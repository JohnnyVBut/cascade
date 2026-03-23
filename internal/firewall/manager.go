// Package firewall manages Firewall Rules that combine packet filtering and
// Policy-Based Routing (PBR) via iptables-nft custom chains.
//
// Architecture (mirrors FirewallManager.js):
//
//   FIREWALL_FORWARD (filter table) — ACCEPT/DROP/REJECT for every rule
//   FIREWALL_MANGLE  (mangle table) — MARK (PBR rules) or RETURN (non-PBR rules)
//
// Rules are processed in order; the first match wins.
// PBR rules: packets get fwmark → ip rule lookup table N → table N has
//   "default via <gatewayIP> dev <iface>" → routed through that gateway.
// Non-PBR rules: RETURN in mangle prevents subsequent PBR rules from marking.
//
// Gateway fallback (FIX-15b):
//   When a gateway goes down, the routing table entry is replaced with either
//   a blackhole route (drop) or the system default gateway (failover),
//   depending on the rule's fallbackToDefault flag.
//   Recovery is delayed 30 s (anti-flap) and triggered by the GatewayMonitor
//   StatusChange callback registered in Init().
//
// Storage: SQLite `firewall_rules` table.
// Source/destination endpoint objects are stored as JSON columns.
package firewall

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/JohnnyVBut/cascade/internal/aliases"
	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/gateway"
	"github.com/JohnnyVBut/cascade/internal/util"
	"github.com/JohnnyVBut/cascade/internal/validate"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Endpoint describes a traffic match for source or destination.
type Endpoint struct {
	Type        string `json:"type"`                  // any | cidr | alias
	Value       string `json:"value,omitempty"`       // CIDR for type=cidr
	AliasID     string `json:"aliasId,omitempty"`     // alias ID for type=alias
	Invert      bool   `json:"invert,omitempty"`      // negate the match (!= )
	Port        string `json:"port,omitempty"`        // legacy plain port string
	PortAliasID string `json:"portAliasId,omitempty"` // port/port-group alias ID
}

// Rule is a firewall/PBR rule persisted in SQLite.
type Rule struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Enabled           bool     `json:"enabled"`
	Order             int      `json:"order"`
	Interface         string   `json:"interface"`         // any | wg10 | eth0 ...
	Protocol          string   `json:"protocol"`          // any | tcp | udp | tcp/udp | icmp
	Source            Endpoint `json:"source"`
	Destination       Endpoint `json:"destination"`
	Action            string   `json:"action"`            // accept | drop | reject
	GatewayID         string   `json:"gatewayId"`         // PBR: direct gateway
	GatewayGroupID    string   `json:"gatewayGroupId"`    // PBR: gateway group
	Fwmark            *int     `json:"fwmark"`            // auto-assigned for PBR rules
	FallbackToDefault bool     `json:"fallbackToDefault"` // fallback to default gw (vs blackhole)
	Log               bool     `json:"log"`
	Comment           string   `json:"comment"`
	CreatedAt         string   `json:"createdAt"`
}

// RuleInput is the create/update request payload from the API.
type RuleInput struct {
	Name              string   `json:"name"`
	Interface         string   `json:"interface"`
	Protocol          string   `json:"protocol"`
	Source            Endpoint `json:"source"`
	Destination       Endpoint `json:"destination"`
	Action            string   `json:"action"`
	GatewayID         string   `json:"gatewayId"`
	GatewayGroupID    string   `json:"gatewayGroupId"`
	Fwmark            *int     `json:"fwmark"`
	FallbackToDefault bool     `json:"fallbackToDefault"`
	Log               bool     `json:"log"`
	Comment           string   `json:"comment"`
}

// portCombo represents one iptables protocol+port combination.
type portCombo struct {
	proto        string // tcp | udp | nil-like ""
	srcPort      string
	srcMultiport bool
	dstPort      string
	dstMultiport bool
}

// TraceStep is one rule evaluated during SimulateTrace.
type TraceStep struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Fwmark   *int   `json:"fwmark"`
	SrcMatch bool   `json:"srcMatch"`
	DstMatch bool   `json:"dstMatch"`
	Matched  bool   `json:"matched"`
}

// TraceResult is the output of SimulateTrace.
type TraceResult struct {
	MatchedRule *MatchedRule `json:"matchedRule"` // nil if no rule matched
	Steps       []TraceStep  `json:"steps"`
}

// MatchedRule is the summary of the winning rule in a trace.
type MatchedRule struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Fwmark *int   `json:"fwmark"`
}

// HostInterface is one network interface returned by GetNetworkInterfaces.
type HostInterface struct {
	Name string `json:"name"`
}

// ── Manager ───────────────────────────────────────────────────────────────────

// Manager manages Firewall Rules and PBR routing tables.
type Manager struct {
	am *aliases.Manager
	gm *gateway.Manager

	rebuildMu sync.Mutex // serialises rebuildChains calls

	fallbackMu     sync.Mutex
	fallbackActive map[string]bool         // rule ID → currently in fallback/blackhole
	restoreTimers  map[string]*time.Timer  // rule ID → 30 s anti-flap restore timer
}

// New creates a Manager. Call Init() after db.Init().
func New(am *aliases.Manager, gm *gateway.Manager) *Manager {
	return &Manager{
		am:             am,
		gm:             gm,
		fallbackActive: make(map[string]bool),
		restoreTimers:  make(map[string]*time.Timer),
	}
}

// Init initialises iptables chains, loads rules from SQLite, rebuilds chains,
// and registers the GatewayMonitor callback for fallback logic.
func (m *Manager) Init() error {
	if err := m.initChains(); err != nil {
		log.Printf("firewall: initChains warning: %v", err)
		// Non-fatal: container may not have iptables on dev machine.
	}

	if err := m.rebuildChains(); err != nil {
		log.Printf("firewall: initial rebuildChains warning: %v", err)
	}

	// Register gateway status change callback for PBR fallback (FIX-15b).
	m.gm.Monitor().OnStatusChange(func(gwID, newStatus, oldStatus string) {
		if err := m.handleGatewayStatusChange(gwID, newStatus, oldStatus); err != nil {
			log.Printf("firewall: handleGatewayStatusChange(%s): %v", gwID, err)
		}
	})

	count, _ := m.countRules()
	log.Printf("firewall: init complete (%d rules)", count)
	return nil
}

// ── Public CRUD ───────────────────────────────────────────────────────────────

// GetRules returns all rules sorted by order ascending.
func (m *Manager) GetRules() ([]Rule, error) {
	rows, err := db.DB().Query(`
		SELECT id, name, enabled, order_idx, interface, protocol,
		       source, destination, action,
		       gateway_id, gateway_group_id, fwmark, fallback_to_default,
		       log, comment, created_at
		FROM firewall_rules ORDER BY order_idx
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRule returns a single rule by ID, or nil if not found.
func (m *Manager) GetRule(id string) (*Rule, error) {
	row := db.DB().QueryRow(`
		SELECT id, name, enabled, order_idx, interface, protocol,
		       source, destination, action,
		       gateway_id, gateway_group_id, fwmark, fallback_to_default,
		       log, comment, created_at
		FROM firewall_rules WHERE id = ?
	`, id)
	r, err := scanRuleRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// AddRule creates a new rule, rebuilds chains, returns the created rule.
func (m *Manager) AddRule(inp RuleInput) (*Rule, error) {
	if err := validateInput(inp); err != nil {
		return nil, err
	}

	order, err := m.nextOrder()
	if err != nil {
		return nil, err
	}

	hasPBR := inp.GatewayID != "" || inp.GatewayGroupID != ""
	fwmark := inp.Fwmark
	if hasPBR && fwmark == nil {
		next, err := m.nextFwmark()
		if err != nil {
			return nil, err
		}
		fwmark = &next
	}
	if !hasPBR {
		fwmark = nil
	}

	rule := Rule{
		ID:                uuid.New().String(),
		Name:              strings.TrimSpace(inp.Name),
		Enabled:           true,
		Order:             order,
		Interface:         strOr(inp.Interface, "any"),
		Protocol:          strOr(inp.Protocol, "any"),
		Source:            normalizeEndpoint(inp.Source),
		Destination:       normalizeEndpoint(inp.Destination),
		Action:            strOr(inp.Action, "accept"),
		GatewayID:         inp.GatewayID,
		GatewayGroupID:    inp.GatewayGroupID,
		Fwmark:            fwmark,
		FallbackToDefault: hasPBR && inp.FallbackToDefault,
		Log:               inp.Log,
		Comment:           strings.TrimSpace(inp.Comment),
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
	}

	if err := insertRule(rule); err != nil {
		return nil, err
	}

	if err := m.rebuildChains(); err != nil {
		log.Printf("firewall: AddRule rebuildChains: %v", err)
	}

	log.Printf("firewall: rule added %q (action=%s order=%d)", rule.Name, rule.Action, rule.Order)
	return &rule, nil
}

// UpdateRule replaces a rule's fields, rebuilds chains, returns the updated rule.
func (m *Manager) UpdateRule(id string, inp RuleInput) (*Rule, error) {
	old, err := m.GetRule(id)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, fmt.Errorf("firewall rule not found")
	}
	if err := validateInput(inp); err != nil {
		return nil, err
	}

	hasPBR := inp.GatewayID != "" || inp.GatewayGroupID != ""
	fwmark := inp.Fwmark
	if hasPBR && fwmark == nil {
		if old.Fwmark != nil {
			fwmark = old.Fwmark
		} else {
			next, err := m.nextFwmark()
			if err != nil {
				return nil, err
			}
			fwmark = &next
		}
	}
	if !hasPBR {
		fwmark = nil
	}

	rule := Rule{
		ID:                old.ID,
		Name:              strings.TrimSpace(inp.Name),
		Enabled:           old.Enabled,
		Order:             old.Order,
		Interface:         strOr(inp.Interface, "any"),
		Protocol:          strOr(inp.Protocol, "any"),
		Source:            normalizeEndpoint(inp.Source),
		Destination:       normalizeEndpoint(inp.Destination),
		Action:            strOr(inp.Action, "accept"),
		GatewayID:         inp.GatewayID,
		GatewayGroupID:    inp.GatewayGroupID,
		Fwmark:            fwmark,
		FallbackToDefault: hasPBR && inp.FallbackToDefault,
		Log:               inp.Log,
		Comment:           strings.TrimSpace(inp.Comment),
		CreatedAt:         old.CreatedAt,
	}

	if err := updateRule(rule); err != nil {
		return nil, err
	}

	if err := m.rebuildChains(); err != nil {
		log.Printf("firewall: UpdateRule rebuildChains: %v", err)
	}

	log.Printf("firewall: rule updated %q", rule.Name)
	return &rule, nil
}

// DeleteRule removes a rule and rebuilds chains.
func (m *Manager) DeleteRule(id string) error {
	r, err := m.GetRule(id)
	if err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("firewall rule not found")
	}

	if _, err := db.DB().Exec(`DELETE FROM firewall_rules WHERE id = ?`, id); err != nil {
		return err
	}

	if err := m.rebuildChains(); err != nil {
		log.Printf("firewall: DeleteRule rebuildChains: %v", err)
	}

	log.Printf("firewall: rule deleted %q", r.Name)
	return nil
}

// ToggleRule enables or disables a rule and rebuilds chains.
func (m *Manager) ToggleRule(id string, enabled bool) (*Rule, error) {
	r, err := m.GetRule(id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("firewall rule not found")
	}

	if _, err := db.DB().Exec(`UPDATE firewall_rules SET enabled = ? WHERE id = ?`, boolInt(enabled), id); err != nil {
		return nil, err
	}

	r.Enabled = enabled

	if err := m.rebuildChains(); err != nil {
		log.Printf("firewall: ToggleRule rebuildChains: %v", err)
	}

	return r, nil
}

// MoveRule swaps the order of a rule with its neighbour ("up" or "down").
func (m *Manager) MoveRule(id, direction string) (*Rule, error) {
	rules, err := m.GetRules()
	if err != nil {
		return nil, err
	}

	idx := -1
	for i, r := range rules {
		if r.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, fmt.Errorf("firewall rule not found")
	}

	swapIdx := idx - 1
	if direction == "down" {
		swapIdx = idx + 1
	}
	if swapIdx < 0 || swapIdx >= len(rules) {
		return &rules[idx], nil // already at edge
	}

	// Swap order values in DB.
	a, b := rules[idx], rules[swapIdx]
	tx, err := db.DB().Begin()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE firewall_rules SET order_idx = ? WHERE id = ?`, b.Order, a.ID); err != nil {
		tx.Rollback()
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE firewall_rules SET order_idx = ? WHERE id = ?`, a.Order, b.ID); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if err := m.rebuildChains(); err != nil {
		log.Printf("firewall: MoveRule rebuildChains: %v", err)
	}

	a.Order, b.Order = b.Order, a.Order
	return &a, nil
}

// SimulateTrace walks the rule list in order, matching srcIP/dstIP against
// each enabled rule. Returns the first matching rule and all evaluated steps.
// Used by the route test API to determine which PBR fwmark (if any) applies.
func (m *Manager) SimulateTrace(srcIP, dstIP string) (*TraceResult, error) {
	rules, err := m.GetRules()
	if err != nil {
		return nil, err
	}

	result := &TraceResult{Steps: []TraceStep{}}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		srcMatch, err := m.matchEndpoint(&rule.Source, srcIP)
		if err != nil {
			srcMatch = false
		}
		dstMatch, err := m.matchEndpoint(&rule.Destination, dstIP)
		if err != nil {
			dstMatch = false
		}
		matched := srcMatch && dstMatch

		result.Steps = append(result.Steps, TraceStep{
			ID:       rule.ID,
			Name:     rule.Name,
			Fwmark:   rule.Fwmark,
			SrcMatch: srcMatch,
			DstMatch: dstMatch,
			Matched:  matched,
		})

		if matched {
			result.MatchedRule = &MatchedRule{
				ID:     rule.ID,
				Name:   rule.Name,
				Fwmark: rule.Fwmark,
			}
			return result, nil
		}
	}
	return result, nil
}

// GetNetworkInterfaces returns host interfaces for the ingress interface selector.
// Parses "ip -o link show" text output — no -j flag (FIX-11).
func (m *Manager) GetNetworkInterfaces() ([]HostInterface, error) {
	out, err := util.ExecSilentFast("ip -o link show")
	if err != nil {
		return nil, err
	}
	var ifaces []HostInterface
	for _, line := range strings.Split(out, "\n") {
		// "2: eth0: <flags>..." or "3: eth0@if2: <flags>..."
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[1])
		if at := strings.Index(name, "@"); at >= 0 {
			name = name[:at]
		}
		if name == "" || name == "lo" {
			continue
		}
		ifaces = append(ifaces, HostInterface{Name: name})
	}
	return ifaces, nil
}

// ── Private: chain management ─────────────────────────────────────────────────

// initChains creates FIREWALL_FORWARD (filter) and FIREWALL_MANGLE (mangle)
// and hooks them at position 1 in their respective base chains (idempotent).
func (m *Manager) initChains() error {
	cmds := []string{
		// filter: FIREWALL_FORWARD
		"iptables-nft -t filter -N FIREWALL_FORWARD 2>/dev/null || true",
		"iptables-nft -t filter -C FORWARD -j FIREWALL_FORWARD 2>/dev/null || iptables-nft -t filter -I FORWARD 1 -j FIREWALL_FORWARD",
		// mangle: FIREWALL_MANGLE
		"iptables-nft -t mangle -N FIREWALL_MANGLE 2>/dev/null || true",
		"iptables-nft -t mangle -C PREROUTING -j FIREWALL_MANGLE 2>/dev/null || iptables-nft -t mangle -I PREROUTING 1 -j FIREWALL_MANGLE",
	}
	for _, cmd := range cmds {
		if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
			log.Printf("firewall: initChains: %s: %v", cmd, err)
		}
	}
	return nil
}

// RebuildChains is the public entry point for rebuildChains.
// Called from main.go after WireGuard interfaces are up, and from interface
// start/restart handlers so that "ip route replace ... dev wgX table N"
// always runs with the interface already in existence.
func (m *Manager) RebuildChains() error {
	return m.rebuildChains()
}

// rebuildChains flushes both chains, cleans up PBR routing, then re-applies
// all enabled rules in order. Also resets fallback state.
func (m *Manager) rebuildChains() error {
	m.rebuildMu.Lock()
	defer m.rebuildMu.Unlock()

	// Reset fallback state — GatewayMonitor will re-emit if gateways are still down.
	m.fallbackMu.Lock()
	for _, t := range m.restoreTimers {
		t.Stop()
	}
	m.restoreTimers = make(map[string]*time.Timer)
	m.fallbackActive = make(map[string]bool)
	m.fallbackMu.Unlock()

	// Flush custom chains.
	util.Exec("iptables-nft -t filter -F FIREWALL_FORWARD", 5*time.Second, true)  //nolint
	util.Exec("iptables-nft -t mangle -F FIREWALL_MANGLE", 5*time.Second, true)   //nolint

	// Clean up PBR routing rules from a previous run.
	if err := m.cleanupRoutingRules(); err != nil {
		log.Printf("firewall: cleanupRoutingRules: %v", err)
	}

	// Re-apply all enabled rules in order.
	rules, err := m.GetRules()
	if err != nil {
		return err
	}

	count := 0
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if err := m.applyRuleKernel(&rule); err != nil {
			log.Printf("firewall: applyRuleKernel %q: %v", rule.Name, err)
		}
		count++
	}

	log.Printf("firewall: chains rebuilt (%d active rules)", count)
	return nil
}

// cleanupRoutingRules removes all ip rule + ip route table entries for PBR rules.
func (m *Manager) cleanupRoutingRules() error {
	rules, err := m.GetRules()
	if err != nil {
		return err
	}
	for _, r := range rules {
		if r.Fwmark == nil {
			continue
		}
		fwmark := *r.Fwmark
		util.Exec(fmt.Sprintf("ip rule del fwmark %d lookup %d", fwmark, fwmark), 5*time.Second, false)    //nolint
		util.Exec(fmt.Sprintf("ip route flush table %d", fwmark), 5*time.Second, false)                    //nolint
	}
	return nil
}

// ── Private: apply rule to kernel ─────────────────────────────────────────────

// applyRuleKernel installs iptables-nft rules for a single firewall rule.
// It computes the cartesian product of port combinations × src endpoints × dst endpoints.
func (m *Manager) applyRuleKernel(rule *Rule) error {
	srcParts, err := m.buildMatchParts("src", &rule.Source)
	if err != nil {
		return err
	}
	dstParts, err := m.buildMatchParts("dst", &rule.Destination)
	if err != nil {
		return err
	}
	combos, err := m.buildPortCombinations(rule)
	if err != nil {
		return err
	}

	// Set up PBR routing once per rule (outside the cartesian product loop).
	if rule.Action == "accept" && (rule.GatewayID != "" || rule.GatewayGroupID != "") {
		if err := m.applyRoutingForRule(rule); err != nil {
			log.Printf("firewall: applyRoutingForRule %q: %v", rule.Name, err)
		}
	}

	for _, combo := range combos {
		for _, srcPart := range srcParts {
			for _, dstPart := range dstParts {
				flags := buildMatchFlags(rule, combo, srcPart, dstPart)

				// Optional LOG target.
				if rule.Log {
					cmd := fmt.Sprintf(`iptables-nft -t filter -A FIREWALL_FORWARD%s -j LOG --log-prefix "FW: "`, flags)
					util.Exec(cmd, 10*time.Second, true) //nolint
				}

				// Mangle MARK (PBR) or RETURN (non-PBR) — in PREROUTING/FIREWALL_MANGLE.
				if rule.Action == "accept" && (rule.GatewayID != "" || rule.GatewayGroupID != "") {
					cmd := fmt.Sprintf("iptables-nft -t mangle -A FIREWALL_MANGLE%s -j MARK --set-mark %d", flags, *rule.Fwmark)
					if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
						log.Printf("firewall: mangle MARK %q: %v", rule.Name, err)
					}
				} else {
					// RETURN prevents downstream PBR rules from marking this traffic.
					cmd := fmt.Sprintf("iptables-nft -t mangle -A FIREWALL_MANGLE%s -j RETURN", flags)
					util.Exec(cmd, 10*time.Second, true) //nolint
				}

				// Filter action.
				target := "ACCEPT"
				switch strings.ToLower(rule.Action) {
				case "drop":
					target = "DROP"
				case "reject":
					target = "REJECT --reject-with icmp-port-unreachable"
				}
				cmd := fmt.Sprintf("iptables-nft -t filter -A FIREWALL_FORWARD%s -j %s", flags, target)
				if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
					log.Printf("firewall: filter %q: %v", rule.Name, err)
				}
			}
		}
	}
	return nil
}

// applyRoutingForRule sets ip route + ip rule for a PBR rule.
// Uses "ip route replace" (idempotent — overwrites stale fallback/blackhole routes).
func (m *Manager) applyRoutingForRule(rule *Rule) error {
	gw, err := m.resolveGateway(rule)
	if err != nil {
		return err
	}

	fwmark := *rule.Fwmark

	// ip route replace default via <gw> dev <iface> onlink table <fwmark>
	// "onlink" bypasses the kernel's reachability check for the next-hop IP.
	// Required when the gateway IP is not in the same subnet as the interface
	// (e.g. a remote KZ server reachable via ens3, or a WireGuard peer whose
	// address lives in a different /24 than the local interface address).
	cmd := fmt.Sprintf("ip route replace default via %s dev %s onlink table %d", gw.gatewayIP, gw.iface, fwmark)
	if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
		return fmt.Errorf("ip route replace: %w", err)
	}

	// ip rule add fwmark <fwmark> lookup <fwmark> — only if not already present.
	if !m.ipRuleExists(fwmark) {
		priority := 1000 + rule.Order*10
		cmd = fmt.Sprintf("ip rule add fwmark %d lookup %d priority %d", fwmark, fwmark, priority)
		if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
			return fmt.Errorf("ip rule add: %w", err)
		}
	}

	return nil
}

// ── Private: match building ───────────────────────────────────────────────────

// buildPortCombinations returns all protocol+port combinations for a rule.
// With port aliases: cartesian product of srcSpecs × dstSpecs (skip incompatible protos).
// Without port aliases: legacy path using rule.Protocol + plain port strings.
func (m *Manager) buildPortCombinations(rule *Rule) ([]portCombo, error) {
	srcHasAlias := rule.Source.PortAliasID != ""
	dstHasAlias := rule.Destination.PortAliasID != ""

	if !srcHasAlias && !dstHasAlias {
		// Legacy path.
		protos := expandProtocol(rule.Protocol)
		combos := make([]portCombo, len(protos))
		for i, proto := range protos {
			combos[i] = portCombo{
				proto:   proto,
				srcPort: strings.TrimSpace(rule.Source.Port),
				dstPort: strings.TrimSpace(rule.Destination.Port),
			}
		}
		return combos, nil
	}

	// Port alias path.
	var srcSpecs []aliases.PortMatchSpec
	if srcHasAlias {
		specs, err := m.am.GetPortMatchSpec(rule.Source.PortAliasID)
		if err != nil {
			return nil, err
		}
		srcSpecs = specs
	} else {
		srcSpecs = []aliases.PortMatchSpec{{Proto: "", Ports: rule.Source.Port, Multiport: false}}
	}

	var dstSpecs []aliases.PortMatchSpec
	if dstHasAlias {
		specs, err := m.am.GetPortMatchSpec(rule.Destination.PortAliasID)
		if err != nil {
			return nil, err
		}
		dstSpecs = specs
	} else {
		dstSpecs = []aliases.PortMatchSpec{{Proto: "", Ports: rule.Destination.Port, Multiport: false}}
	}

	var combos []portCombo
	for _, src := range srcSpecs {
		for _, dst := range dstSpecs {
			// Skip incompatible protocols (a packet cannot be both TCP and UDP).
			if src.Proto != "" && dst.Proto != "" && src.Proto != dst.Proto {
				continue
			}
			proto := src.Proto
			if proto == "" {
				proto = dst.Proto
			}
			combos = append(combos, portCombo{
				proto:        proto,
				srcPort:      src.Ports,
				srcMultiport: src.Multiport,
				dstPort:      dst.Ports,
				dstMultiport: dst.Multiport,
			})
		}
	}

	if len(combos) == 0 {
		// All combos were filtered — return a no-protocol fallback.
		return []portCombo{{}}, nil
	}
	return combos, nil
}

// buildMatchFlags constructs the iptables match flag string for one cartesian cell.
// Example: " -i wg10 -p tcp -s 10.0.0.0/8 --sport 80 -d 8.8.8.8 --dport 53"
func buildMatchFlags(rule *Rule, combo portCombo, srcPart, dstPart string) string {
	var sb strings.Builder
	if rule.Interface != "" && rule.Interface != "any" {
		fmt.Fprintf(&sb, " -i %s", rule.Interface)
	}
	if combo.proto != "" {
		fmt.Fprintf(&sb, " -p %s", combo.proto)
	}
	if srcPart != "" {
		sb.WriteByte(' ')
		sb.WriteString(srcPart)
	}
	if combo.srcPort != "" && combo.proto != "" {
		sb.WriteString(portPartStr("--sport", combo.srcPort, combo.srcMultiport))
	}
	if dstPart != "" {
		sb.WriteByte(' ')
		sb.WriteString(dstPart)
	}
	if combo.dstPort != "" && combo.proto != "" {
		sb.WriteString(portPartStr("--dport", combo.dstPort, combo.dstMultiport))
	}
	return sb.String()
}

// portPartStr returns "--dport 443" or "-m multiport --dports 80,443".
func portPartStr(flag, ports string, multiport bool) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(ports), "-", ":")
	// Use multiport module when there are multiple ports or explicit multiport flag.
	if multiport || strings.Contains(normalized, ",") {
		pluralFlag := "--dports"
		if flag == "--sport" {
			pluralFlag = "--sports"
		}
		return fmt.Sprintf(" -m multiport %s %s", pluralFlag, normalized)
	}
	return fmt.Sprintf(" %s %s", flag, normalized)
}

// expandProtocol maps rule.Protocol to a slice of iptables protocol strings.
// "tcp/udp" → ["tcp","udp"], "any" → [""], "tcp" → ["tcp"]
func expandProtocol(protocol string) []string {
	if protocol == "" || protocol == "any" {
		return []string{""}
	}
	if protocol == "tcp/udp" {
		return []string{"tcp", "udp"}
	}
	return []string{protocol}
}

// buildMatchParts returns iptables source/destination match fragments for an endpoint.
// Returns [""] for "any" (no -s/-d flag), ["-s CIDR"] for cidr, or expanded alias entries.
func (m *Manager) buildMatchParts(dir string, ep *Endpoint) ([]string, error) {
	flag := "-s"
	matchDir := "src"
	if dir == "dst" {
		flag = "-d"
		matchDir = "dst"
	}
	invert := ""
	if ep.Invert {
		invert = "! "
	}

	if ep == nil || ep.Type == "" || ep.Type == "any" {
		return []string{""}, nil
	}

	if ep.Type == "cidr" {
		return []string{fmt.Sprintf("%s%s %s", invert, flag, ep.Value)}, nil
	}

	if ep.Type == "alias" {
		spec, err := m.am.GetMatchSpec(ep.AliasID)
		if err != nil || spec == nil {
			log.Printf("firewall: alias %s not found, skipping match", ep.AliasID)
			return []string{""}, nil
		}
		if spec.Type == "ipset" {
			inv := ""
			if ep.Invert {
				inv = "! "
			}
			return []string{fmt.Sprintf("-m set %s--match-set %s %s", inv, spec.Name, matchDir)}, nil
		}
		// CIDR-based alias: one fragment per entry.
		if len(spec.Entries) == 0 {
			return []string{""}, nil
		}
		parts := make([]string, len(spec.Entries))
		for i, cidr := range spec.Entries {
			parts[i] = fmt.Sprintf("%s%s %s", invert, flag, cidr)
		}
		return parts, nil
	}

	return []string{""}, nil
}

// ── Private: gateway resolution ───────────────────────────────────────────────

type resolvedGW struct {
	gatewayIP string
	iface     string
}

// resolveGateway finds the active gateway for a PBR rule.
// For a group, picks the first-tier member (lowest tier number).
func (m *Manager) resolveGateway(rule *Rule) (resolvedGW, error) {
	if rule.GatewayID != "" {
		gw, err := m.gm.GetGateway(rule.GatewayID)
		if err != nil || gw == nil {
			return resolvedGW{}, fmt.Errorf("gateway %s not found", rule.GatewayID)
		}
		return resolvedGW{gatewayIP: gw.GatewayIP, iface: gw.Interface}, nil
	}

	if rule.GatewayGroupID != "" {
		grp, err := m.gm.GetGroup(rule.GatewayGroupID)
		if err != nil || grp == nil {
			return resolvedGW{}, fmt.Errorf("gateway group %s not found", rule.GatewayGroupID)
		}
		if len(grp.Gateways) == 0 {
			return resolvedGW{}, fmt.Errorf("gateway group %s is empty", rule.GatewayGroupID)
		}
		// Sort by tier (lowest = highest priority), pick first.
		best := grp.Gateways[0]
		for _, m := range grp.Gateways[1:] {
			if m.Tier < best.Tier {
				best = m
			}
		}
		gw, err := m.gm.GetGateway(best.GatewayID)
		if err != nil || gw == nil {
			return resolvedGW{}, fmt.Errorf("gateway %s not found in group", best.GatewayID)
		}
		return resolvedGW{gatewayIP: gw.GatewayIP, iface: gw.Interface}, nil
	}

	return resolvedGW{}, fmt.Errorf("rule has no gateway or gateway group")
}

// ipRuleExists checks whether an ip rule for fwmark already exists.
// Parses "ip rule show" text output (FIX-11).
func (m *Manager) ipRuleExists(fwmark int) bool {
	out, err := util.Exec("ip rule show", 5*time.Second, false)
	if err != nil {
		return false
	}
	hex := fmt.Sprintf("0x%x", fwmark)
	dec := fmt.Sprintf("%d", fwmark)
	return strings.Contains(out, "fwmark "+hex) || strings.Contains(out, "fwmark "+dec+" ")
}

// ── Private: gateway fallback ─────────────────────────────────────────────────

// handleGatewayStatusChange is the GatewayMonitor callback (FIX-15b).
func (m *Manager) handleGatewayStatusChange(gatewayID, newStatus, oldStatus string) error {
	if newStatus == "down" && oldStatus != "down" {
		return m.onGatewayDown(gatewayID)
	}
	if newStatus != "down" && oldStatus == "down" {
		return m.onGatewayUp(gatewayID)
	}
	return nil
}

// onGatewayDown triggers fallback for all PBR rules using the downed gateway.
func (m *Manager) onGatewayDown(gatewayID string) error {
	rules, err := m.GetRules()
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if !rule.Enabled || rule.Fwmark == nil {
			continue
		}
		if rule.GatewayID == gatewayID {
			m.triggerFallback(&rule, fmt.Sprintf("gateway %s down", gatewayID))
			continue
		}
		if rule.GatewayGroupID != "" {
			allDown, _ := m.isGroupAllDown(rule.GatewayGroupID)
			if allDown {
				m.triggerFallback(&rule, fmt.Sprintf("all gateways in group %s down", rule.GatewayGroupID))
			}
		}
	}
	return nil
}

// onGatewayUp schedules a 30 s delayed route restore for all rules in fallback.
func (m *Manager) onGatewayUp(gatewayID string) error {
	rules, err := m.GetRules()
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if !rule.Enabled || rule.Fwmark == nil {
			continue
		}
		m.fallbackMu.Lock()
		inFallback := m.fallbackActive[rule.ID]
		m.fallbackMu.Unlock()

		if !inFallback {
			continue
		}

		shouldSchedule := rule.GatewayID == gatewayID
		if !shouldSchedule && rule.GatewayGroupID != "" {
			allDown, _ := m.isGroupAllDown(rule.GatewayGroupID)
			if !allDown { // at least one member is back up
				shouldSchedule = true
			}
		}

		if shouldSchedule {
			ruleCopy := rule
			m.fallbackMu.Lock()
			if t, ok := m.restoreTimers[rule.ID]; ok {
				t.Stop()
			}
			log.Printf("firewall: rule %q: scheduling route restore in 30s", rule.Name)
			m.restoreTimers[rule.ID] = time.AfterFunc(30*time.Second, func() {
				m.fallbackMu.Lock()
				delete(m.restoreTimers, ruleCopy.ID)
				m.fallbackMu.Unlock()

				if err := m.restoreRoute(&ruleCopy); err != nil {
					log.Printf("firewall: restoreRoute %q: %v", ruleCopy.Name, err)
				}
			})
			m.fallbackMu.Unlock()
		}
	}
	return nil
}

// triggerFallback installs a blackhole or default-gateway route for table N.
func (m *Manager) triggerFallback(rule *Rule, reason string) {
	m.fallbackMu.Lock()
	if m.fallbackActive[rule.ID] {
		m.fallbackMu.Unlock()
		return
	}
	// Cancel pending restore timer if any.
	if t, ok := m.restoreTimers[rule.ID]; ok {
		t.Stop()
		delete(m.restoreTimers, rule.ID)
	}
	m.fallbackMu.Unlock()

	fwmark := *rule.Fwmark

	if rule.FallbackToDefault {
		gw, err := m.getSystemDefaultGateway()
		if err != nil {
			log.Printf("firewall: triggerFallback: cannot get system default gw: %v", err)
			return
		}
		cmd := fmt.Sprintf("ip route replace default via %s dev %s onlink table %d", gw.gatewayIP, gw.iface, fwmark)
		if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
			log.Printf("firewall: triggerFallback: %v", err)
			return
		}
		log.Printf("firewall: rule %q: fallback → default via %s (%s)", rule.Name, gw.gatewayIP, reason)
	} else {
		cmd := fmt.Sprintf("ip route replace blackhole default table %d", fwmark)
		if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
			log.Printf("firewall: triggerFallback: blackhole: %v", err)
			return
		}
		log.Printf("firewall: rule %q: blackhole ACTIVE (%s)", rule.Name, reason)
	}

	m.fallbackMu.Lock()
	m.fallbackActive[rule.ID] = true
	m.fallbackMu.Unlock()
}

// restoreRoute reinstates the original gateway route after recovery.
func (m *Manager) restoreRoute(rule *Rule) error {
	gw, err := m.resolveGateway(rule)
	if err != nil {
		return err
	}
	fwmark := *rule.Fwmark
	cmd := fmt.Sprintf("ip route replace default via %s dev %s onlink table %d", gw.gatewayIP, gw.iface, fwmark)
	if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
		return err
	}

	m.fallbackMu.Lock()
	delete(m.fallbackActive, rule.ID)
	m.fallbackMu.Unlock()

	log.Printf("firewall: rule %q: route RESTORED via %s", rule.Name, gw.gatewayIP)
	return nil
}

// getSystemDefaultGateway parses "ip route show default" for the host's default gw.
// Uses text output (FIX-11): "default via 192.168.1.1 dev eth0 ..."
func (m *Manager) getSystemDefaultGateway() (resolvedGW, error) {
	out, err := util.Exec("ip route show default", 5*time.Second, false)
	if err != nil {
		return resolvedGW{}, err
	}
	// Parse: "default via <ip> dev <iface> ..."
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		// fields: [default, via, IP, dev, IFACE, ...]
		if len(fields) >= 5 && fields[0] == "default" && fields[1] == "via" && fields[3] == "dev" {
			return resolvedGW{gatewayIP: fields[2], iface: fields[4]}, nil
		}
	}
	return resolvedGW{}, fmt.Errorf("system default gateway not found in: %q", out)
}

// isGroupAllDown returns true when every member of a gateway group has status "down".
func (m *Manager) isGroupAllDown(groupID string) (bool, error) {
	grp, err := m.gm.GetGroup(groupID)
	if err != nil {
		return true, err
	}
	if grp == nil || len(grp.Gateways) == 0 {
		return true, nil
	}
	for _, member := range grp.Gateways {
		st := m.gm.Monitor().GetStatus(member.GatewayID)
		if st.Status != "down" {
			return false, nil
		}
	}
	return true, nil
}

// ── Private: simulateTrace helpers ────────────────────────────────────────────

// matchEndpoint checks whether ip matches the endpoint condition.
func (m *Manager) matchEndpoint(ep *Endpoint, ip string) (bool, error) {
	if ep == nil || ep.Type == "" || ep.Type == "any" {
		return true, nil
	}

	var rawMatch bool
	var err error

	switch ep.Type {
	case "cidr":
		rawMatch = ipInCIDR(ip, ep.Value)
	case "alias":
		rawMatch, err = m.matchAlias(ep.AliasID, ip)
		if err != nil {
			return false, err
		}
	}

	if ep.Invert {
		return !rawMatch, nil
	}
	return rawMatch, nil
}

// matchAlias checks whether ip is a member of the named alias.
func (m *Manager) matchAlias(aliasID, ip string) (bool, error) {
	spec, err := m.am.GetMatchSpec(aliasID)
	if err != nil || spec == nil {
		return false, nil
	}
	if spec.Type == "ipset" {
		return m.ipsetTest(spec.Name, ip), nil
	}
	for _, cidr := range spec.Entries {
		if ipInCIDR(ip, cidr) {
			return true, nil
		}
	}
	return false, nil
}

// ipsetTest runs "ipset test <name> <ip>" and returns true on exit 0.
// Both setName and ip are validated before shell interpolation (CRIT-2).
func (m *Manager) ipsetTest(setName, ip string) bool {
	if err := validate.IpsetName(setName); err != nil {
		return false
	}
	if err := validate.IP(ip); err != nil {
		return false
	}
	_, err := util.Exec(fmt.Sprintf("ipset test %s %s", setName, ip), 3*time.Second, false)
	return err == nil
}

// ipInCIDR reports whether ip falls within cidr (e.g. "10.0.0.0/8").
// Uses net.ParseCIDR + ipNet.Contains — correct and handles host-bits-set CIDRs.
//
// Previous implementation used bits.RotateLeft32(^uint32(0), -prefixLen) for
// the subnet mask. Rotating all-ones by any amount always returns all-ones
// (0xFFFFFFFF), so the mask was never applied and only exact-address matches
// succeeded. This broke SimulateTrace CIDR source matching (FIX-GO-10).
func ipInCIDR(ipStr, cidr string) bool {
	if cidr == "" || ipStr == "" {
		return false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	if !strings.Contains(cidr, "/") {
		// Host address without prefix — exact match.
		return ip.Equal(net.ParseIP(cidr))
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return ipNet.Contains(ip)
}

// ── Private: DB helpers ───────────────────────────────────────────────────────

type ruleScanner interface {
	Scan(dest ...any) error
}

func scanRule(rows *sql.Rows) (Rule, error) {
	r, err := scanRuleRow(rows)
	if err != nil {
		return Rule{}, err
	}
	return *r, nil
}

func scanRuleRow(s ruleScanner) (*Rule, error) {
	var r Rule
	var enabled, fallback, logVal int
	var srcJSON, dstJSON string
	var fwmark sql.NullInt64

	err := s.Scan(
		&r.ID, &r.Name, &enabled, &r.Order, &r.Interface, &r.Protocol,
		&srcJSON, &dstJSON, &r.Action,
		&r.GatewayID, &r.GatewayGroupID, &fwmark, &fallback,
		&logVal, &r.Comment, &r.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	r.Enabled = enabled != 0
	r.FallbackToDefault = fallback != 0
	r.Log = logVal != 0

	if fwmark.Valid {
		v := int(fwmark.Int64)
		r.Fwmark = &v
	}

	if srcJSON != "" && srcJSON != "{}" {
		_ = json.Unmarshal([]byte(srcJSON), &r.Source)
	}
	if dstJSON != "" && dstJSON != "{}" {
		_ = json.Unmarshal([]byte(dstJSON), &r.Destination)
	}

	// Normalise action to lowercase.
	r.Action = strings.ToLower(r.Action)
	if r.Action == "" {
		r.Action = "accept"
	}

	return &r, nil
}

func insertRule(r Rule) error {
	srcJSON, _ := json.Marshal(r.Source)
	dstJSON, _ := json.Marshal(r.Destination)

	var fwmark interface{}
	if r.Fwmark != nil {
		fwmark = *r.Fwmark
	}

	_, err := db.DB().Exec(`
		INSERT INTO firewall_rules
		    (id, name, enabled, order_idx, interface, protocol,
		     source, destination, action,
		     gateway_id, gateway_group_id, fwmark, fallback_to_default,
		     log, comment, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ID, r.Name, boolInt(r.Enabled), r.Order, r.Interface, r.Protocol,
		string(srcJSON), string(dstJSON), r.Action,
		r.GatewayID, r.GatewayGroupID, fwmark, boolInt(r.FallbackToDefault),
		boolInt(r.Log), r.Comment, r.CreatedAt,
	)
	return err
}

func updateRule(r Rule) error {
	srcJSON, _ := json.Marshal(r.Source)
	dstJSON, _ := json.Marshal(r.Destination)

	var fwmark interface{}
	if r.Fwmark != nil {
		fwmark = *r.Fwmark
	}

	_, err := db.DB().Exec(`
		UPDATE firewall_rules
		SET name = ?, enabled = ?, interface = ?, protocol = ?,
		    source = ?, destination = ?, action = ?,
		    gateway_id = ?, gateway_group_id = ?, fwmark = ?,
		    fallback_to_default = ?, log = ?, comment = ?
		WHERE id = ?
	`,
		r.Name, boolInt(r.Enabled), r.Interface, r.Protocol,
		string(srcJSON), string(dstJSON), r.Action,
		r.GatewayID, r.GatewayGroupID, fwmark,
		boolInt(r.FallbackToDefault), boolInt(r.Log), r.Comment,
		r.ID,
	)
	return err
}

// ── Private: validation + helpers ─────────────────────────────────────────────

func validateInput(inp RuleInput) error {
	if strings.TrimSpace(inp.Name) == "" {
		return fmt.Errorf("rule name is required")
	}
	if inp.Action != "" {
		switch strings.ToLower(inp.Action) {
		case "accept", "drop", "reject":
		default:
			return fmt.Errorf("action must be accept, drop, or reject")
		}
	}
	if inp.Interface != "" && inp.Interface != "any" {
		for _, c := range inp.Interface {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
				c == '.' || c == '-' || c == '_') {
				return fmt.Errorf("invalid interface name")
			}
		}
	}
	if inp.Protocol != "" {
		switch inp.Protocol {
		case "any", "tcp", "udp", "tcp/udp", "icmp":
		default:
			return fmt.Errorf("protocol must be any, tcp, udp, tcp/udp, or icmp")
		}
	}
	if inp.Source.Type == "cidr" && strings.TrimSpace(inp.Source.Value) == "" {
		return fmt.Errorf("source CIDR value is required")
	}
	if inp.Destination.Type == "cidr" && strings.TrimSpace(inp.Destination.Value) == "" {
		return fmt.Errorf("destination CIDR value is required")
	}
	return nil
}

// normalizeEndpoint sanitises and fills defaults for an endpoint.
func normalizeEndpoint(ep Endpoint) Endpoint {
	if ep.Type == "" || ep.Type == "any" {
		return Endpoint{Type: "any"}
	}
	ep.Value = strings.TrimSpace(ep.Value)
	ep.AliasID = strings.TrimSpace(ep.AliasID)
	ep.Port = strings.TrimSpace(ep.Port)
	ep.PortAliasID = strings.TrimSpace(ep.PortAliasID)
	return ep
}

// nextOrder returns max(order) + 1 across all existing rules.
func (m *Manager) nextOrder() (int, error) {
	var max int
	err := db.DB().QueryRow(`SELECT COALESCE(MAX(order_idx), 0) FROM firewall_rules`).Scan(&max)
	return max + 1, err
}

// nextFwmark returns the smallest integer >= 1000 not already used as a fwmark.
func (m *Manager) nextFwmark() (int, error) {
	rows, err := db.DB().Query(`SELECT fwmark FROM firewall_rules WHERE fwmark IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	used := make(map[int]bool)
	for rows.Next() {
		var v int
		if rows.Scan(&v) == nil {
			used[v] = true
		}
	}

	mark := 1000
	for used[mark] {
		mark++
	}
	return mark, nil
}

// countRules returns the number of rules stored.
func (m *Manager) countRules() (int, error) {
	var n int
	err := db.DB().QueryRow(`SELECT COUNT(*) FROM firewall_rules`).Scan(&n)
	return n, err
}

// boolInt converts bool to 0/1 for SQLite.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// strOr returns s if non-empty, otherwise def.
func strOr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// ── Singleton accessor ────────────────────────────────────────────────────────

var fwInstance *Manager

// SetInstance stores the initialized Manager for package-level access.
// Must be called from main() before serving requests.
func SetInstance(m *Manager) { fwInstance = m }

// Get returns the package-level Manager singleton.
// Panics with a clear message if SetInstance was not called (programming error).
func Get() *Manager {
	if fwInstance == nil {
		panic("firewall: manager not initialized — call SetInstance before Get()")
	}
	return fwInstance
}
