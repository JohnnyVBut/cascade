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
	"github.com/JohnnyVBut/cascade/internal/settings"
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
// RuleType is "rule" (default) or "separator" (visual divider, ignored by kernel).
type Rule struct {
	ID                string   `json:"id"`
	RuleType          string   `json:"ruleType"`          // "rule" | "separator"
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
	SeparatorColor    string   `json:"separatorColor"`    // separator tint: ""=gray | red|orange|yellow|green|cyan|blue|purple
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
	ID           string `json:"id"`
	Name         string `json:"name"`
	Fwmark       *int   `json:"fwmark"`
	SrcMatch     bool   `json:"srcMatch"`
	DstMatch     bool   `json:"dstMatch"`
	Matched      bool   `json:"matched"`
	ProtoSkipped bool   `json:"protoSkipped,omitempty"` // true when rule has protocol/port condition that was not evaluated
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

	// Ensure the applied snapshot exists (first run after upgrade: copy draft → applied).
	if err := m.ensureAppliedSnapshot(); err != nil {
		log.Printf("firewall: ensureAppliedSnapshot warning: %v", err)
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

// GetRules returns all rules sorted by order ascending (includes separators).
func (m *Manager) GetRules() ([]Rule, error) {
	rows, err := db.DB().Query(`
		SELECT id, rule_type, name, enabled, order_idx, interface, protocol,
		       source, destination, action,
		       gateway_id, gateway_group_id, fwmark, fallback_to_default,
		       log, comment, separator_color, created_at
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
		SELECT id, rule_type, name, enabled, order_idx, interface, protocol,
		       source, destination, action,
		       gateway_id, gateway_group_id, fwmark, fallback_to_default,
		       log, comment, separator_color, created_at
		FROM firewall_rules WHERE id = ?
	`, id)
	r, err := scanRuleRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// ── Staged apply ──────────────────────────────────────────────────────────────

// getAppliedRules reads rules from the applied snapshot table (used by rebuildChains).
func (m *Manager) getAppliedRules() ([]Rule, error) {
	rows, err := db.DB().Query(`
		SELECT id, rule_type, name, enabled, order_idx, interface, protocol,
		       source, destination, action,
		       gateway_id, gateway_group_id, fwmark, fallback_to_default,
		       log, comment, separator_color, created_at
		FROM firewall_rules_applied ORDER BY order_idx
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

// ensureAppliedSnapshot copies firewall_rules → firewall_rules_applied when the
// applied table is empty. Called on Init() to handle first-run and upgrades from
// pre-staged versions where no snapshot exists yet.
func (m *Manager) ensureAppliedSnapshot() error {
	var count int
	if err := db.DB().QueryRow(`SELECT COUNT(*) FROM firewall_rules_applied`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil // snapshot already exists
	}
	// First run or upgrade: seed applied from draft so existing rules stay active.
	_, err := db.DB().Exec(`
		INSERT INTO firewall_rules_applied
		    (id, rule_type, name, interface, protocol, source, destination, src_port, dst_port,
		     action, gateway_id, gateway_group_id, fwmark, fallback_to_default,
		     enabled, log, comment, separator_color, order_idx, created_at)
		SELECT id, rule_type, name, interface, protocol, source, destination, src_port, dst_port,
		       action, gateway_id, gateway_group_id, fwmark, fallback_to_default,
		       enabled, log, comment, separator_color, order_idx, created_at
		FROM firewall_rules
	`)
	if err != nil {
		return fmt.Errorf("ensureAppliedSnapshot: %w", err)
	}
	log.Printf("firewall: applied snapshot seeded from draft (%d rules)", count)
	return nil
}

// ApplyRules copies the current draft (firewall_rules) → applied snapshot, then
// rebuilds iptables chains from the snapshot.
func (m *Manager) ApplyRules() error {
	tx, err := db.DB().Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM firewall_rules_applied`); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO firewall_rules_applied
		    (id, rule_type, name, interface, protocol, source, destination, src_port, dst_port,
		     action, gateway_id, gateway_group_id, fwmark, fallback_to_default,
		     enabled, log, comment, separator_color, order_idx, created_at)
		SELECT id, rule_type, name, interface, protocol, source, destination, src_port, dst_port,
		       action, gateway_id, gateway_group_id, fwmark, fallback_to_default,
		       enabled, log, comment, separator_color, order_idx, created_at
		FROM firewall_rules
	`); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("firewall: rules applied (snapshot updated)")
	return m.rebuildChains()
}

// DiscardChanges overwrites the draft (firewall_rules) with the applied snapshot,
// reverting all unapplied edits. No kernel change is needed (kernel already matches applied).
func (m *Manager) DiscardChanges() error {
	tx, err := db.DB().Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM firewall_rules`); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO firewall_rules
		    (id, rule_type, name, interface, protocol, source, destination, src_port, dst_port,
		     action, gateway_id, gateway_group_id, fwmark, fallback_to_default,
		     enabled, log, comment, separator_color, order_idx, created_at)
		SELECT id, rule_type, name, interface, protocol, source, destination, src_port, dst_port,
		       action, gateway_id, gateway_group_id, fwmark, fallback_to_default,
		       enabled, log, comment, separator_color, order_idx, created_at
		FROM firewall_rules_applied
	`); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("firewall: draft discarded — reverted to applied snapshot")
	return nil
}

// HasPendingChanges reports whether the draft differs from the applied snapshot.
// NOTE: SQLite compound operators (EXCEPT, UNION ALL) are left-associative and
// have equal precedence, so we must wrap each EXCEPT in a subquery to avoid the
// mis-parse: A EXCEPT B UNION ALL C EXCEPT D → ((A EXCEPT B) UNION ALL C) EXCEPT D.
func (m *Manager) HasPendingChanges() (bool, error) {
	var diff int
	err := db.DB().QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT * FROM (
				SELECT id, rule_type, name, enabled, order_idx, interface, protocol,
				       source, destination, action, gateway_id, gateway_group_id,
				       fwmark, fallback_to_default, log, comment, separator_color
				FROM firewall_rules
				WHERE rule_type != 'separator'
				EXCEPT
				SELECT id, rule_type, name, enabled, order_idx, interface, protocol,
				       source, destination, action, gateway_id, gateway_group_id,
				       fwmark, fallback_to_default, log, comment, separator_color
				FROM firewall_rules_applied
				WHERE rule_type != 'separator'
			) AS draft_minus_applied
			UNION ALL
			SELECT * FROM (
				SELECT id, rule_type, name, enabled, order_idx, interface, protocol,
				       source, destination, action, gateway_id, gateway_group_id,
				       fwmark, fallback_to_default, log, comment, separator_color
				FROM firewall_rules_applied
				WHERE rule_type != 'separator'
				EXCEPT
				SELECT id, rule_type, name, enabled, order_idx, interface, protocol,
				       source, destination, action, gateway_id, gateway_group_id,
				       fwmark, fallback_to_default, log, comment, separator_color
				FROM firewall_rules
				WHERE rule_type != 'separator'
			) AS applied_minus_draft
		)
	`).Scan(&diff)
	if err != nil {
		return false, err
	}
	return diff > 0, nil
}

// AddRule creates a new rule, returns the created rule.
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

	log.Printf("firewall: rule added %q (action=%s order=%d) — pending apply", rule.Name, rule.Action, rule.Order)
	return &rule, nil
}

// allowedSeparatorColors is the set of valid separator color values.
// Empty string means "default" (gray). Must match the frontend palette.
var allowedSeparatorColors = map[string]bool{
	"": true, "red": true, "orange": true, "yellow": true,
	"green": true, "cyan": true, "blue": true, "purple": true,
}

// sanitizeSeparatorColor returns color if it is in the allowed palette, "" otherwise.
func sanitizeSeparatorColor(color string) string {
	if allowedSeparatorColors[color] {
		return color
	}
	return ""
}

// Separate INSERT constants to avoid fmt.Sprintf table-name interpolation antipattern.
const insertSepDraft = `
	INSERT INTO firewall_rules (id, rule_type, name, enabled, order_idx, interface, protocol,
	    source, destination, action, gateway_id, gateway_group_id, fwmark,
	    fallback_to_default, log, comment, separator_color, created_at)
	VALUES (?, 'separator', ?, 1, ?, 'any', 'any', '{}', '{}', 'accept', '', '', NULL, 0, 0, ?, ?, ?)
`
const insertSepApplied = `
	INSERT INTO firewall_rules_applied (id, rule_type, name, enabled, order_idx, interface, protocol,
	    source, destination, action, gateway_id, gateway_group_id, fwmark,
	    fallback_to_default, log, comment, separator_color, created_at)
	VALUES (?, 'separator', ?, 1, ?, 'any', 'any', '{}', '{}', 'accept', '', '', NULL, 0, 0, ?, ?, ?)
`

// AddSeparator creates a visual separator (no iptables effect) at the end of the list.
// Writes to BOTH firewall_rules and firewall_rules_applied so separators are never
// part of the pending-apply cycle.
func (m *Manager) AddSeparator(name, color string) (*Rule, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Separator"
	}
	color = sanitizeSeparatorColor(color)
	order, err := m.nextOrder()
	if err != nil {
		return nil, err
	}
	sep := Rule{
		ID:             uuid.New().String(),
		RuleType:       "separator",
		Name:           name,
		SeparatorColor: color,
		Enabled:        true,
		Order:          order,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	tx, err := db.DB().Begin()
	if err != nil {
		return nil, err
	}
	args := []any{sep.ID, sep.Name, sep.Order, "", sep.SeparatorColor, sep.CreatedAt}
	if _, err := tx.Exec(insertSepDraft, args...); err != nil {
		tx.Rollback()
		return nil, err
	}
	if _, err := tx.Exec(insertSepApplied, args...); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	log.Printf("firewall: separator added %q color=%q (order=%d)", sep.Name, sep.Color(), sep.Order)
	return &sep, nil
}

// UpdateSeparator updates name and color of a separator, syncing both tables.
// Returns an error if the rule does not exist or is not a separator.
func (m *Manager) UpdateSeparator(id, name, color string) (*Rule, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Separator"
	}
	color = sanitizeSeparatorColor(color)
	tx, err := db.DB().Begin()
	if err != nil {
		return nil, err
	}
	res, err := tx.Exec(
		`UPDATE firewall_rules SET name = ?, separator_color = ? WHERE id = ? AND rule_type = 'separator'`,
		name, color, id,
	)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		tx.Rollback()
		return nil, fmt.Errorf("firewall rule not found")
	}
	if _, err := tx.Exec(
		`UPDATE firewall_rules_applied SET name = ?, separator_color = ? WHERE id = ? AND rule_type = 'separator'`,
		name, color, id,
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m.GetRule(id)
}

// Color returns SeparatorColor for logging convenience.
func (r *Rule) Color() string { return r.SeparatorColor }

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

	log.Printf("firewall: rule updated %q — pending apply", rule.Name)
	return &rule, nil
}

// DeleteRule removes a rule from the draft (pending apply).
// For separators: also removes from firewall_rules_applied (separators bypass the apply cycle).
func (m *Manager) DeleteRule(id string) error {
	r, err := m.GetRule(id)
	if err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("firewall rule not found")
	}

	if r.RuleType == "separator" {
		tx, err := db.DB().Begin()
		if err != nil {
			return err
		}
		for _, tbl := range []string{"firewall_rules", "firewall_rules_applied"} {
			if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, tbl), id); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		log.Printf("firewall: separator deleted %q", r.Name)
		return nil
	}

	if _, err := db.DB().Exec(`DELETE FROM firewall_rules WHERE id = ?`, id); err != nil {
		return err
	}
	log.Printf("firewall: rule deleted %q — pending apply", r.Name)
	return nil
}

// ToggleRule enables or disables a rule (pending apply).
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
	return r, nil
}

// MoveRule swaps the order of a rule with its neighbour ("up" or "down") — pending apply.
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
	// If either swapped item is a separator, sync BOTH order_idx values to applied.
	// Rationale: moving a separator doesn't change the relative order of non-separator
	// rules, so it must not create a "pending changes" diff. Because HasPendingChanges
	// compares order_idx for non-separators, the neighbor's order_idx must also be
	// kept in sync when it was displaced only by a separator move.
	if a.RuleType == "separator" || b.RuleType == "separator" {
		if _, err := tx.Exec(`UPDATE firewall_rules_applied SET order_idx = ? WHERE id = ?`, b.Order, a.ID); err != nil {
			tx.Rollback()
			return nil, err
		}
		if _, err := tx.Exec(`UPDATE firewall_rules_applied SET order_idx = ? WHERE id = ?`, a.Order, b.ID); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	a.Order, b.Order = b.Order, a.Order
	return &a, nil
}

// ReorderRules sets the order_idx of rules to match the given slice of IDs.
// The slice must contain exactly all existing rule IDs (no extras, no missing).
// If only separators changed position (non-separator relative order is unchanged),
// the reorder is synced to applied immediately and creates no pending diff.
// If non-separators changed position, only separator order_idx is synced to applied
// (normal pending apply for the non-separator changes).
func (m *Manager) ReorderRules(ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("ids must not be empty")
	}

	// Fetch current rules to validate and detect separator positions.
	rules, err := m.GetRules()
	if err != nil {
		return err
	}

	// Validate: incoming list must be exactly the same set of IDs as in the DB.
	if len(ids) != len(rules) {
		return fmt.Errorf("reorder list length %d does not match rule count %d", len(ids), len(rules))
	}
	knownIDs := make(map[string]bool, len(rules))
	for _, r := range rules {
		knownIDs[r.ID] = true
	}
	for _, id := range ids {
		if !knownIDs[id] {
			return fmt.Errorf("unknown rule id %q in reorder list", id)
		}
	}

	// Build separator set and old non-separator sequence (by ID, in current order).
	sepIDs := make(map[string]bool)
	oldNonSepSeq := make([]string, 0, len(rules))
	for _, r := range rules {
		if r.RuleType == "separator" {
			sepIDs[r.ID] = true
		} else {
			oldNonSepSeq = append(oldNonSepSeq, r.ID)
		}
	}
	// New non-separator sequence from the incoming ids list.
	newNonSepSeq := make([]string, 0, len(oldNonSepSeq))
	for _, id := range ids {
		if !sepIDs[id] {
			newNonSepSeq = append(newNonSepSeq, id)
		}
	}
	// If relative order of non-separators is unchanged → only separators moved.
	onlySepsMoved := len(oldNonSepSeq) == len(newNonSepSeq)
	if onlySepsMoved {
		for i := range oldNonSepSeq {
			if oldNonSepSeq[i] != newNonSepSeq[i] {
				onlySepsMoved = false
				break
			}
		}
	}

	tx, err := db.DB().Begin()
	if err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE firewall_rules SET order_idx = ? WHERE id = ?`, i, id); err != nil {
			tx.Rollback()
			return err
		}
	}

	// Always sync separator order_idx to applied (separators bypass the apply cycle).
	if _, err := tx.Exec(`
		UPDATE firewall_rules_applied
		SET order_idx = (SELECT order_idx FROM firewall_rules WHERE firewall_rules.id = firewall_rules_applied.id)
		WHERE id IN (SELECT id FROM firewall_rules WHERE rule_type = 'separator')
	`); err != nil {
		tx.Rollback()
		return err
	}

	if onlySepsMoved {
		// Non-separator relative order unchanged → also sync non-separator order_idx
		// so HasPendingChanges does not produce a false positive.
		if _, err := tx.Exec(`
			UPDATE firewall_rules_applied
			SET order_idx = (SELECT order_idx FROM firewall_rules WHERE firewall_rules.id = firewall_rules_applied.id)
			WHERE id IN (SELECT id FROM firewall_rules WHERE rule_type != 'separator')
		`); err != nil {
			tx.Rollback()
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	if onlySepsMoved {
		log.Printf("firewall: rules reordered (%d rules) — separators only, no pending diff", len(ids))
	} else {
		log.Printf("firewall: rules reordered (%d rules) — pending apply", len(ids))
	}
	return nil
}

// SimulateTrace walks the rule list in order, matching srcIP/dstIP against
// each enabled rule. Returns the first matching rule and all evaluated steps.
// Used by the route test API to determine which PBR fwmark (if any) applies.
//
// Rules with protocol or port conditions are recorded in the trace with
// ProtoSkipped=true and never counted as matched — the caller only provides
// L3 addresses, so L4 conditions cannot be evaluated. When protocol/port
// parameters are added to the trace in the future, pass them here and evaluate
// them before the srcMatch/dstMatch block.
func (m *Manager) SimulateTrace(srcIP, dstIP string) (*TraceResult, error) {
	// Prefer the applied snapshot so results match what's actually in iptables.
	// Fall back to live rules when the snapshot is empty (e.g. in tests).
	rules, err := m.getAppliedRules()
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		rules, err = m.GetRules()
		if err != nil {
			return nil, err
		}
	}

	result := &TraceResult{Steps: []TraceStep{}}
	for _, rule := range rules {
		if !rule.Enabled || rule.RuleType == "separator" {
			continue
		}

		// Rules with protocol or port conditions cannot be evaluated with L3-only
		// input. Record them in the trace as skipped so the UI can show them, but
		// never count them as matched — they must not block subsequent rules from
		// being evaluated.
		hasProto := rule.Protocol != "" && rule.Protocol != "any"
		hasPort := rule.Source.Port != "" || rule.Source.PortAliasID != "" ||
			rule.Destination.Port != "" || rule.Destination.PortAliasID != ""
		if hasProto || hasPort {
			result.Steps = append(result.Steps, TraceStep{
				ID:           rule.ID,
				Name:         rule.Name,
				Fwmark:       rule.Fwmark,
				ProtoSkipped: true,
			})
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

// FlushAll flushes and removes Cascade-owned iptables chains.
// Used before a full restore so no stale rules survive the DB replacement.
func (m *Manager) FlushAll() {
	cmds := []string{
		"iptables-nft -t filter -F FIREWALL_FORWARD 2>/dev/null || true",
		"iptables-nft -t filter -D FORWARD -j FIREWALL_FORWARD 2>/dev/null || true",
		"iptables-nft -t filter -X FIREWALL_FORWARD 2>/dev/null || true",
		"iptables-nft -t mangle -F FIREWALL_MANGLE 2>/dev/null || true",
		"iptables-nft -t mangle -D PREROUTING -j FIREWALL_MANGLE 2>/dev/null || true",
		"iptables-nft -t mangle -X FIREWALL_MANGLE 2>/dev/null || true",
	}
	for _, cmd := range cmds {
		util.Exec(cmd, 5*time.Second, true) //nolint:errcheck
	}
	log.Printf("firewall: FlushAll: Cascade chains removed")
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

	// Remove per-rule subchains from previous run (FW*/FM* created by applyRuleKernelSubchain).
	m.cleanupSubchains()

	// Always allow ESTABLISHED/RELATED traffic first — return packets from the internet
	// to VPN clients must pass even when default policy is DROP.
	util.Exec("iptables-nft -t filter -A FIREWALL_FORWARD -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT", 5*time.Second, true) //nolint

	// Clean up PBR routing rules from a previous run.
	if err := m.cleanupRoutingRules(); err != nil {
		log.Printf("firewall: cleanupRoutingRules: %v", err)
	}

	// Re-apply all enabled rules in order — always from the applied snapshot.
	rules, err := m.getAppliedRules()
	if err != nil {
		return err
	}

	count := 0
	ruleErrors := 0
	for _, rule := range rules {
		if rule.RuleType == "separator" {
			continue // separators are visual-only, no iptables commands
		}
		if !rule.Enabled {
			continue
		}
		if err := m.applyRuleKernel(&rule); err != nil {
			log.Printf("firewall: applyRuleKernel %q: %v", rule.Name, err)
			ruleErrors++
		}
		count++
	}

	// Terminal default policy for FIREWALL_FORWARD.
	// Added AFTER all per-rule ACCEPT/DROP/REJECT commands so it is always last.
	gs, err := settings.GetSettings()
	if err != nil {
		// DB error — cannot determine policy. Log and leave chain without terminal rule
		// (effective policy remains ACCEPT via base chain).
		log.Printf("firewall: rebuildChains: failed to read default policy: %v — terminal rule not appended", err)
	} else if gs.DefaultFwPolicy == "drop" {
		// M2: warn if any rule failed to install — a failed ACCEPT rule combined with
		// a terminal DROP may silently block traffic that was expected to be permitted.
		if ruleErrors > 0 {
			log.Printf("firewall: WARNING: default policy is DROP but %d rule(s) failed to install — some expected ACCEPT rules may be missing; verify connectivity", ruleErrors)
		}
		util.Exec("iptables-nft -t filter -A FIREWALL_FORWARD -j DROP", 5*time.Second, true) //nolint
		log.Printf("firewall: default policy DROP appended to FIREWALL_FORWARD")
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
//
// iptables-nft limitation: a single rule cannot combine native nft expressions
// (protocol/port matches like "udp dport 53") with xt_compat expressions (ipset
// via "-m set --match-set"). The xt_compat match is silently dropped.
// When both are present, applyRuleKernelSubchain is used instead: address/ipset
// matching stays in FIREWALL_FORWARD (xt_compat only), port matching moves to a
// per-rule subchain (native nft only).
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

	// Use subchain approach when mixing port matches (native nft) with ipset matches
	// (xt_compat) to avoid silent ipset-match loss.
	if anyComboHasPort(combos) && (anyPartIsIpset(srcParts) || anyPartIsIpset(dstParts)) {
		return m.applyRuleKernelSubchain(rule, combos, srcParts, dstParts)
	}

	isPBR := rule.Action == "accept" && (rule.GatewayID != "" || rule.GatewayGroupID != "")
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
				if isPBR {
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
				cmd := fmt.Sprintf("iptables-nft -t filter -A FIREWALL_FORWARD%s -j %s", flags, ruleTarget(rule))
				if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
					log.Printf("firewall: filter %q: %v", rule.Name, err)
				}
			}
		}
	}
	// PBR first-match semantics: once a mark is set, stop processing so that
	// subsequent (more general) PBR rules cannot override it.
	if isPBR {
		util.Exec("iptables-nft -t mangle -A FIREWALL_MANGLE -m mark ! --mark 0 -j RETURN", 10*time.Second, true) //nolint
	}
	return nil
}

// applyRuleKernelSubchain handles rules that combine ipset address matching with
// protocol/port matching. iptables-nft cannot combine these in one rule.
//
// Structure:
//
//	FIREWALL_FORWARD: -m set --match-set <ipset> src  →  JUMP FW<id8>   (xt_compat only)
//	FW<id8>:          -p udp --dport 53               →  ACCEPT          (native nft only)
//	FW<id8>:          (no match)                      →  RETURN
func (m *Manager) applyRuleKernelSubchain(rule *Rule, combos []portCombo, srcParts, dstParts []string) error {
	// Subchain name: "FW"/"FM" + first 8 hex chars of rule UUID (length = 10).
	shortID := strings.ReplaceAll(rule.ID, "-", "")[:8]
	filterChain := "FW" + shortID
	mangleChain := "FM" + shortID
	isPBR := rule.Action == "accept" && (rule.GatewayID != "" || rule.GatewayGroupID != "")

	// Create and flush subchains (idempotent).
	for _, tc := range []struct{ table, chain string }{{"filter", filterChain}, {"mangle", mangleChain}} {
		util.Exec(fmt.Sprintf("iptables-nft -t %s -N %s 2>/dev/null || true", tc.table, tc.chain), 5*time.Second, true)
		util.Exec(fmt.Sprintf("iptables-nft -t %s -F %s", tc.table, tc.chain), 5*time.Second, true)
	}

	// Populate subchains with port-only rules (no address/ipset — avoids mixing).
	for _, combo := range combos {
		portFlags := buildPortFlags(combo)

		// Filter subchain: optional LOG + action.
		if rule.Log {
			cmd := fmt.Sprintf(`iptables-nft -t filter -A %s%s -j LOG --log-prefix "FW: "`, filterChain, portFlags)
			util.Exec(cmd, 10*time.Second, true) //nolint
		}
		cmd := fmt.Sprintf("iptables-nft -t filter -A %s%s -j %s", filterChain, portFlags, ruleTarget(rule))
		if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
			log.Printf("firewall: subchain filter %q: %v", rule.Name, err)
		}

		// Mangle subchain: MARK (PBR) or RETURN.
		if isPBR {
			cmd = fmt.Sprintf("iptables-nft -t mangle -A %s%s -j MARK --set-mark %d", mangleChain, portFlags, *rule.Fwmark)
			if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
				log.Printf("firewall: subchain mangle MARK %q: %v", rule.Name, err)
			}
		} else {
			cmd = fmt.Sprintf("iptables-nft -t mangle -A %s%s -j RETURN", mangleChain, portFlags)
			util.Exec(cmd, 10*time.Second, true) //nolint
		}
	}
	// Terminal RETURN: port not matched → fall through to next rule in FIREWALL_FORWARD.
	util.Exec(fmt.Sprintf("iptables-nft -t filter -A %s -j RETURN", filterChain), 5*time.Second, true)
	util.Exec(fmt.Sprintf("iptables-nft -t mangle -A %s -j RETURN", mangleChain), 5*time.Second, true)

	// FIREWALL_FORWARD / FIREWALL_MANGLE: address-only matches → jump to subchains.
	// No port/proto here — xt_compat (ipset) remains isolated from native nft (port).
	for _, srcPart := range srcParts {
		for _, dstPart := range dstParts {
			addrFlags := buildAddrFlags(rule, srcPart, dstPart)

			cmd := fmt.Sprintf("iptables-nft -t filter -A FIREWALL_FORWARD%s -j %s", addrFlags, filterChain)
			if _, err := util.Exec(cmd, 10*time.Second, true); err != nil {
				log.Printf("firewall: subchain jump filter %q: %v", rule.Name, err)
			}
			cmd = fmt.Sprintf("iptables-nft -t mangle -A FIREWALL_MANGLE%s -j %s", addrFlags, mangleChain)
			util.Exec(cmd, 10*time.Second, true) //nolint
		}
	}
	// PBR first-match semantics: once a mark is set, stop processing so that
	// subsequent (more general) PBR rules cannot override it.
	if isPBR {
		util.Exec("iptables-nft -t mangle -A FIREWALL_MANGLE -m mark ! --mark 0 -j RETURN", 10*time.Second, true) //nolint
	}
	return nil
}

// cleanupSubchains removes all FW*/FM* per-rule subchains left from a previous run.
// Called at the start of rebuildChains before re-applying rules.
func (m *Manager) cleanupSubchains() {
	for _, tc := range []struct{ table, prefix string }{{"filter", "FW"}, {"mangle", "FM"}} {
		out, err := util.Exec(fmt.Sprintf("iptables-nft -t %s -S", tc.table), 5*time.Second, false)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			parts := strings.Fields(line)
			// Lines like "-N FWa1b2c3d4" — our chains are exactly 10 chars (prefix 2 + id 8).
			if len(parts) >= 2 && parts[0] == "-N" &&
				strings.HasPrefix(parts[1], tc.prefix) && len(parts[1]) == 10 {
				chain := parts[1]
				util.Exec(fmt.Sprintf("iptables-nft -t %s -F %s 2>/dev/null || true", tc.table, chain), 5*time.Second, true)
				util.Exec(fmt.Sprintf("iptables-nft -t %s -X %s 2>/dev/null || true", tc.table, chain), 5*time.Second, true)
			}
		}
	}
}

// applyRoutingForRule sets ip route + ip rule for a PBR rule.
// Uses "ip route replace" (idempotent — overwrites stale fallback/blackhole routes).
func (m *Manager) applyRoutingForRule(rule *Rule) error {
	gw, err := m.resolveGateway(rule)
	if err != nil {
		return err
	}

	fwmark := *rule.Fwmark

	// WireGuard/AmneziaWG interfaces are point-to-point — the kernel rejects
	// "via <ip> onlink" with "Nexthop has invalid gateway". Route via dev only.
	// For regular interfaces (ens*, eth*) keep "via <ip> onlink" so the kernel
	// knows which next-hop to use when multiple routes exist in the table.
	isWG := strings.HasPrefix(gw.iface, "wg") || strings.HasPrefix(gw.iface, "awg")
	var cmd string
	if isWG || gw.gatewayIP == "" {
		cmd = fmt.Sprintf("ip route replace default dev %s table %d", gw.iface, fwmark)
	} else {
		cmd = fmt.Sprintf("ip route replace default via %s dev %s onlink table %d", gw.gatewayIP, gw.iface, fwmark)
	}
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
		srcPort := strings.TrimSpace(rule.Source.Port)
		dstPort := strings.TrimSpace(rule.Destination.Port)
		hasPort := srcPort != "" || dstPort != ""

		protos := expandProtocol(rule.Protocol)
		var combos []portCombo
		for _, proto := range protos {
			if proto == "" && hasPort {
				// iptables requires -p <proto> before --sport/--dport.
				// When protocol is "any" but ports are specified, expand to both
				// tcp and udp so the port constraint is actually applied.
				for _, p := range []string{"tcp", "udp"} {
					combos = append(combos, portCombo{proto: p, srcPort: srcPort, dstPort: dstPort})
				}
			} else {
				combos = append(combos, portCombo{proto: proto, srcPort: srcPort, dstPort: dstPort})
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
	// Port matches MUST come immediately after -p <proto>, before any -m set or address
	// matches. iptables-nft splits the rule incorrectly when --sport/--dport appear after
	// -m set, producing two separate rules (one with the set match, one bare) and losing
	// the port constraint entirely.
	if combo.proto != "" {
		if combo.srcPort != "" {
			sb.WriteString(portPartStr("--sport", combo.srcPort, combo.srcMultiport))
		}
		if combo.dstPort != "" {
			sb.WriteString(portPartStr("--dport", combo.dstPort, combo.dstMultiport))
		}
	}
	if srcPart != "" {
		sb.WriteByte(' ')
		sb.WriteString(srcPart)
	}
	if dstPart != "" {
		sb.WriteByte(' ')
		sb.WriteString(dstPart)
	}
	return sb.String()
}

// ruleTarget returns the iptables target string for a rule's action.
func ruleTarget(rule *Rule) string {
	switch strings.ToLower(rule.Action) {
	case "drop":
		return "DROP"
	case "reject":
		return "REJECT --reject-with icmp-port-unreachable"
	default:
		return "ACCEPT"
	}
}

// anyComboHasPort reports whether any portCombo has a src or dst port match
// that will actually be emitted (requires proto to be set — iptables cannot
// match ports without -p <proto>).
func anyComboHasPort(combos []portCombo) bool {
	for _, c := range combos {
		if c.proto != "" && (c.srcPort != "" || c.dstPort != "") {
			return true
		}
	}
	return false
}

// anyPartIsIpset reports whether any match-part string uses an ipset (-m set) match.
func anyPartIsIpset(parts []string) bool {
	for _, p := range parts {
		if strings.Contains(p, "--match-set") {
			return true
		}
	}
	return false
}

// buildAddrFlags builds match flags for interface and address/ipset matches only
// (no protocol or port). Used for the FIREWALL_FORWARD jump entry in the subchain approach.
func buildAddrFlags(rule *Rule, srcPart, dstPart string) string {
	var sb strings.Builder
	if rule.Interface != "" && rule.Interface != "any" {
		fmt.Fprintf(&sb, " -i %s", rule.Interface)
	}
	if srcPart != "" {
		sb.WriteByte(' ')
		sb.WriteString(srcPart)
	}
	if dstPart != "" {
		sb.WriteByte(' ')
		sb.WriteString(dstPart)
	}
	return sb.String()
}

// buildPortFlags builds match flags for protocol and port only (no address or interface).
// Used for rules inside per-rule subchains.
func buildPortFlags(combo portCombo) string {
	var sb strings.Builder
	if combo.proto != "" {
		fmt.Fprintf(&sb, " -p %s", combo.proto)
	}
	if combo.proto != "" {
		if combo.srcPort != "" {
			sb.WriteString(portPartStr("--sport", combo.srcPort, combo.srcMultiport))
		}
		if combo.dstPort != "" {
			sb.WriteString(portPartStr("--dport", combo.dstPort, combo.dstMultiport))
		}
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
		if err := validateCIDROrIP(ep.Value); err != nil {
			return nil, fmt.Errorf("buildMatchParts cidr: %w", err)
		}
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
	isDown := func(s string) bool { return s == "down" || s == "admin_down" }
	if isDown(newStatus) && !isDown(oldStatus) {
		return m.onGatewayDown(gatewayID)
	}
	if !isDown(newStatus) && isDown(oldStatus) {
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

// isGroupAllDown returns true when every member of a gateway group has status "down" or "admin_down".
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
		if st.Status != "down" && st.Status != "admin_down" {
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
		// Return an error so the caller skips Invert logic — otherwise a missing
		// alias with Invert=true would produce !false=true (spurious match).
		return false, fmt.Errorf("alias %s not found", aliasID)
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
		&r.ID, &r.RuleType, &r.Name, &enabled, &r.Order, &r.Interface, &r.Protocol,
		&srcJSON, &dstJSON, &r.Action,
		&r.GatewayID, &r.GatewayGroupID, &fwmark, &fallback,
		&logVal, &r.Comment, &r.SeparatorColor, &r.CreatedAt,
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

	// Normalise rule_type.
	if r.RuleType == "" {
		r.RuleType = "rule"
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

	ruleType := r.RuleType
	if ruleType == "" {
		ruleType = "rule"
	}
	_, err := db.DB().Exec(`
		INSERT INTO firewall_rules
		    (id, rule_type, name, enabled, order_idx, interface, protocol,
		     source, destination, action,
		     gateway_id, gateway_group_id, fwmark, fallback_to_default,
		     log, comment, separator_color, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ID, ruleType, r.Name, boolInt(r.Enabled), r.Order, r.Interface, r.Protocol,
		string(srcJSON), string(dstJSON), r.Action,
		r.GatewayID, r.GatewayGroupID, fwmark, boolInt(r.FallbackToDefault),
		boolInt(r.Log), r.Comment, r.SeparatorColor, r.CreatedAt,
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
	if inp.Source.Type == "cidr" {
		if strings.TrimSpace(inp.Source.Value) == "" {
			return fmt.Errorf("source CIDR value is required")
		}
		if err := validateCIDROrIP(inp.Source.Value); err != nil {
			return fmt.Errorf("source: %w", err)
		}
	}
	if inp.Destination.Type == "cidr" {
		if strings.TrimSpace(inp.Destination.Value) == "" {
			return fmt.Errorf("destination CIDR value is required")
		}
		if err := validateCIDROrIP(inp.Destination.Value); err != nil {
			return fmt.Errorf("destination: %w", err)
		}
	}
	return nil
}

// validateCIDROrIP rejects any value that is not a valid IP address or CIDR notation.
// This prevents command injection when the value is interpolated into iptables commands.
func validateCIDROrIP(s string) error {
	s = strings.TrimSpace(s)
	if net.ParseIP(s) != nil {
		return nil
	}
	if _, _, err := net.ParseCIDR(s); err == nil {
		return nil
	}
	return fmt.Errorf("invalid IP/CIDR value %q", s)
}

// normalizeEndpoint sanitises and fills defaults for an endpoint.
// Port fields are preserved even for type=any — a rule can match "any source,
// but only on port 53" which is perfectly valid.
func normalizeEndpoint(ep Endpoint) Endpoint {
	ep.Port = strings.TrimSpace(ep.Port)
	ep.PortAliasID = strings.TrimSpace(ep.PortAliasID)
	if ep.Type == "" || ep.Type == "any" {
		return Endpoint{
			Type:        "any",
			Port:        ep.Port,
			PortAliasID: ep.PortAliasID,
		}
	}
	ep.Value = strings.TrimSpace(ep.Value)
	ep.AliasID = strings.TrimSpace(ep.AliasID)
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

// TryGet returns the package-level Manager singleton, or nil if not yet initialized.
// Prefer this over Get() in code that may run before SetInstance (e.g. tests).
func TryGet() *Manager { return fwInstance }
