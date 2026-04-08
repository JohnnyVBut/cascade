// Package nat manages Source NAT rules via iptables-nft POSTROUTING.
//
// Rules are persisted in the SQLite `nat_rules` table (migration v1).
// On startup, RestoreAll() re-applies all enabled rules to the kernel.
//
// Rule model:
//
//	id            — UUID primary key
//	name          — display name
//	source        — '' (any) | 'x.x.x.x/yy' (subnet) | 'x.x.x.x' (IP)
//	source_alias_id — alias id ('' if direct CIDR/IP)
//	out_interface — outbound host interface (eth0, wg10, …)
//	type          — 'MASQUERADE' | 'SNAT'
//	to_source     — target IP for SNAT ('' for MASQUERADE)
//	comment       — optional note
//	enabled       — 0/1
//
// Generated iptables-nft commands:
//
//	# MASQUERADE (any source):
//	iptables-nft -t nat -A POSTROUTING -o eth0 -j MASQUERADE
//
//	# MASQUERADE (specific subnet):
//	iptables-nft -t nat -A POSTROUTING -s 10.8.0.0/24 -o eth0 -j MASQUERADE
//
//	# SNAT:
//	iptables-nft -t nat -A POSTROUTING -s 10.8.0.0/24 -o eth0 -j SNAT --to-source 1.2.3.4
//
// Deletion uses the same arguments with -A replaced by -D.
// Idempotency: -C (check) is run before -A to avoid duplicates on container restart (FIX-14).
package nat

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/JohnnyVBut/cascade/internal/aliases"
	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/util"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// NatRule is a Source NAT rule stored in SQLite.
// Auto and InterfaceID are set only for virtual auto-rules synthesized from tunnel
// interfaces at request time; they are never stored in the database.
type NatRule struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Source        string `json:"source"`        // '' | CIDR | IP
	SourceAliasID string `json:"sourceAliasId"` // '' if direct CIDR/IP
	OutInterface  string `json:"outInterface"`
	Type          string `json:"type"`      // "MASQUERADE" | "SNAT"
	ToSource      string `json:"toSource"`  // '' for MASQUERADE
	Comment       string `json:"comment"`
	Enabled       bool   `json:"enabled"`
	OrderIdx      int    `json:"orderIdx"`
	CreatedAt     string `json:"createdAt"`
	// Auto is true for virtual rules synthesized from tunnel PostUp MASQUERADE lines.
	// omitempty: not emitted for regular DB-backed rules (zero value = false).
	Auto        bool   `json:"auto,omitempty"`
	InterfaceID string `json:"interfaceId,omitempty"`
}

// IfaceInfo is a minimal description of a tunnel interface passed to GetAutoNatRules.
// It avoids an import cycle between nat and tunnel packages: the API handler reads
// tunnel.TunnelInterface and converts it to []IfaceInfo before calling this method.
type IfaceInfo struct {
	ID            string
	Name          string
	Address       string // CIDR e.g. "10.8.0.1/24"
	Enabled       bool
	NatDisabled   bool
	DisableRoutes bool
}

// NatRuleInput is the create/update request payload.
type NatRuleInput struct {
	Name          string `json:"name"`
	Source        string `json:"source"`
	SourceAliasID string `json:"sourceAliasId"`
	OutInterface  string `json:"outInterface"`
	Type          string `json:"type"`
	ToSource      string `json:"toSource"`
	Comment       string `json:"comment"`
}

// HostInterface is one item returned by GetNetworkInterfaces.
type HostInterface struct {
	Name string `json:"name"`
}

// Manager manages Source NAT rules.
type Manager struct {
	am *aliases.Manager // may be nil in tests
}

// New creates a Manager. Call RestoreAll() after construction.
// am may be nil; alias-based source resolution is skipped in that case.
func New(am *aliases.Manager) *Manager {
	return &Manager{am: am}
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

// RestoreAll applies all enabled NAT rules to the kernel.
// Must be called after InterfaceManager has brought up WireGuard interfaces (FIX-13).
// Uses -C idempotency: rules already present in the kernel are not duplicated (FIX-14).
func (m *Manager) RestoreAll() {
	rules, err := m.GetRules()
	if err != nil {
		log.Printf("nat: RestoreAll: failed to load rules: %v", err)
		return
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if err := m.applyRule(&rule); err != nil {
			log.Printf("nat: RestoreAll: failed to restore rule %q: %v", rule.Name, err)
		} else {
			log.Printf("nat: restored rule %q", rule.Name)
		}
	}
	m.RestoreAllDnat()
}

// ── Public API ────────────────────────────────────────────────────────────────

// GetNetworkInterfaces returns host network interfaces for outbound interface selection.
// Parses "ip -o link show" text output — no -j flag (FIX-11).
func (m *Manager) GetNetworkInterfaces() ([]HostInterface, error) {
	out, err := util.ExecFast("ip -o link show")
	if err != nil {
		return nil, fmt.Errorf("ip -o link show: %w", err)
	}
	var ifaces []HostInterface
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Line format: "2: eth0: <flags> mtu ..." or "3: eth0@if2: <flags> ..."
		// Split on ":" — first field is index, second is name.
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[1])
		// Strip @xxx suffix (e.g. "eth0@if2" in container namespaces).
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

// GetRules returns all NAT rules ordered by order_idx, created_at.
func (m *Manager) GetRules() ([]NatRule, error) {
	rows, err := db.DB().Query(`
		SELECT id, name, source, source_alias_id, out_interface, type,
		       to_source, comment, enabled, order_idx, created_at
		FROM nat_rules
		ORDER BY order_idx, created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []NatRule
	for rows.Next() {
		var r NatRule
		var enabled int
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Source, &r.SourceAliasID, &r.OutInterface,
			&r.Type, &r.ToSource, &r.Comment, &enabled, &r.OrderIdx, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// GetAutoNatRules builds virtual read-only NatRule entries representing the MASQUERADE
// PostUp rules that are automatically generated by tunnel interfaces.
//
// A virtual auto rule is created for each interface where:
//   - Enabled=true (interface is up — the iptables rule only exists while running)
//   - Address != "" (no Address → no PostUp block at all)
//   - DisableRoutes=false (S2S interconnect interfaces must not be NATted — FIX-1)
//   - NatDisabled=false (user has not explicitly opted out)
//
// The nat package never imports tunnel to avoid an import cycle.
// The API handler converts []*tunnel.TunnelInterface to []IfaceInfo and passes it here.
func (m *Manager) GetAutoNatRules(ifaces []IfaceInfo) []NatRule {
	out := []NatRule{}
	for _, iface := range ifaces {
		if !iface.Enabled || iface.Address == "" || iface.DisableRoutes || iface.NatDisabled {
			continue
		}
		// Derive the network address from the interface CIDR (e.g. "10.8.0.1/24" → "10.8.0.0/24").
		subnet := iface.Address
		if _, ipNet, err := net.ParseCIDR(iface.Address); err == nil {
			subnet = ipNet.String()
		}
		out = append(out, NatRule{
			ID:           "auto:" + iface.ID,
			Name:         iface.Name + " (auto)",
			Source:       subnet,
			OutInterface: "$ISP",
			Type:         "MASQUERADE",
			Enabled:      true,
			Auto:         true,
			InterfaceID:  iface.ID,
		})
	}
	return out
}

// GetRule returns a single NAT rule by ID, or nil if not found.
func (m *Manager) GetRule(id string) (*NatRule, error) {
	var r NatRule
	var enabled int
	err := db.DB().QueryRow(`
		SELECT id, name, source, source_alias_id, out_interface, type,
		       to_source, comment, enabled, order_idx, created_at
		FROM nat_rules WHERE id = ?
	`, id).Scan(
		&r.ID, &r.Name, &r.Source, &r.SourceAliasID, &r.OutInterface,
		&r.Type, &r.ToSource, &r.Comment, &enabled, &r.OrderIdx, &r.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.Enabled = enabled != 0
	return &r, nil
}

// AddRule creates a new NAT rule and applies it to the kernel immediately.
func (m *Manager) AddRule(inp NatRuleInput) (*NatRule, error) {
	if err := m.validate(inp); err != nil {
		return nil, err
	}

	rule := NatRule{
		ID:            uuid.New().String(),
		Name:          strings.TrimSpace(inp.Name),
		Source:        strIf(inp.SourceAliasID != "", "", strings.TrimSpace(inp.Source)),
		SourceAliasID: strings.TrimSpace(inp.SourceAliasID),
		OutInterface:  strings.TrimSpace(inp.OutInterface),
		Type:          inp.Type,
		ToSource:      strIf(inp.Type == "SNAT", strings.TrimSpace(inp.ToSource), ""),
		Comment:       strings.TrimSpace(inp.Comment),
		Enabled:       true,
		OrderIdx:      0,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}

	if err := m.applyRule(&rule); err != nil {
		return nil, err
	}

	_, err := db.DB().Exec(`
		INSERT INTO nat_rules
		    (id, name, source, source_alias_id, out_interface, type,
		     to_source, comment, enabled, order_idx, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rule.ID, rule.Name, rule.Source, rule.SourceAliasID, rule.OutInterface,
		rule.Type, rule.ToSource, rule.Comment, boolInt(rule.Enabled), rule.OrderIdx, rule.CreatedAt,
	)
	if err != nil {
		// Best-effort rollback of the kernel rule.
		_ = m.removeRule(&rule)
		return nil, err
	}

	log.Printf("nat: rule added: %q (%s via %s)", rule.Name, rule.Type, rule.OutInterface)
	return &rule, nil
}

// UpdateRule replaces an existing NAT rule.
// Removes the old kernel rule (if enabled) then applies the updated one.
func (m *Manager) UpdateRule(id string, inp NatRuleInput) (*NatRule, error) {
	old, err := m.GetRule(id)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, fmt.Errorf("nat rule not found")
	}
	if err := m.validate(inp); err != nil {
		return nil, err
	}

	updated := NatRule{
		ID:            old.ID,
		Name:          strings.TrimSpace(inp.Name),
		Source:        strIf(inp.SourceAliasID != "", "", strings.TrimSpace(inp.Source)),
		SourceAliasID: strings.TrimSpace(inp.SourceAliasID),
		OutInterface:  strings.TrimSpace(inp.OutInterface),
		Type:          inp.Type,
		ToSource:      strIf(inp.Type == "SNAT", strings.TrimSpace(inp.ToSource), ""),
		Comment:       strings.TrimSpace(inp.Comment),
		Enabled:       old.Enabled,
		OrderIdx:      old.OrderIdx,
		CreatedAt:     old.CreatedAt,
	}

	// Remove old kernel rule if it was active.
	if old.Enabled {
		if err := m.removeRule(old); err != nil {
			log.Printf("nat: UpdateRule: remove old rule %q failed (may already be gone): %v", old.Name, err)
		}
	}

	// Apply updated rule if still enabled.
	if updated.Enabled {
		if err := m.applyRule(&updated); err != nil {
			return nil, err
		}
	}

	_, err = db.DB().Exec(`
		UPDATE nat_rules
		SET name = ?, source = ?, source_alias_id = ?, out_interface = ?,
		    type = ?, to_source = ?, comment = ?
		WHERE id = ?
	`,
		updated.Name, updated.Source, updated.SourceAliasID, updated.OutInterface,
		updated.Type, updated.ToSource, updated.Comment, id,
	)
	if err != nil {
		return nil, err
	}

	log.Printf("nat: rule updated: %q", updated.Name)
	return &updated, nil
}

// DeleteRule removes a NAT rule from the kernel and the database.
func (m *Manager) DeleteRule(id string) error {
	rule, err := m.GetRule(id)
	if err != nil {
		return err
	}
	if rule == nil {
		return fmt.Errorf("nat rule not found")
	}

	if rule.Enabled {
		if err := m.removeRule(rule); err != nil {
			log.Printf("nat: DeleteRule: kernel remove for %q failed (may already be gone): %v", rule.Name, err)
		}
	}

	if _, err := db.DB().Exec(`DELETE FROM nat_rules WHERE id = ?`, id); err != nil {
		return err
	}

	log.Printf("nat: rule deleted: %q", rule.Name)
	return nil
}

// ToggleRule enables or disables a NAT rule.
// Applies to / removes from kernel as needed.
func (m *Manager) ToggleRule(id string, enabled bool) (*NatRule, error) {
	rule, err := m.GetRule(id)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		return nil, fmt.Errorf("nat rule not found")
	}

	if enabled && !rule.Enabled {
		if err := m.applyRule(rule); err != nil {
			return nil, err
		}
	} else if !enabled && rule.Enabled {
		if err := m.removeRule(rule); err != nil {
			return nil, err
		}
	}

	if _, err := db.DB().Exec(`UPDATE nat_rules SET enabled = ? WHERE id = ?`, boolInt(enabled), id); err != nil {
		return nil, err
	}

	rule.Enabled = enabled
	return rule, nil
}

// ── Private ───────────────────────────────────────────────────────────────────

// validate checks NatRuleInput for required fields and safe values.
func (m *Manager) validate(inp NatRuleInput) error {
	if strings.TrimSpace(inp.Name) == "" {
		return fmt.Errorf("rule name is required")
	}
	iface := strings.TrimSpace(inp.OutInterface)
	if iface == "" {
		return fmt.Errorf("outbound interface is required")
	}
	// Allow only letters, digits, dots, hyphens, underscores — prevent shell injection.
	for _, c := range iface {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '-' || c == '_') {
			return fmt.Errorf("invalid interface name")
		}
	}
	if inp.Type != "MASQUERADE" && inp.Type != "SNAT" {
		return fmt.Errorf("type must be MASQUERADE or SNAT")
	}
	if inp.Type == "SNAT" && strings.TrimSpace(inp.ToSource) == "" {
		return fmt.Errorf("SNAT requires a target IP (toSource)")
	}
	// Validate source CIDR/IP if specified and not an alias.
	if inp.SourceAliasID == "" {
		src := strings.TrimSpace(inp.Source)
		if src != "" && !isIPOrCIDR(src) {
			return fmt.Errorf("invalid source address or CIDR")
		}
	}
	// Validate SNAT target IP.
	if inp.Type == "SNAT" {
		ip := strings.TrimSpace(inp.ToSource)
		if !isIPv4Addr(ip) {
			return fmt.Errorf("invalid SNAT target IP")
		}
	}
	return nil
}

// buildCmds constructs iptables-nft commands for a rule.
// action: "A" (append), "D" (delete), "C" (check).
func (m *Manager) buildCmds(rule *NatRule, action string) ([]string, error) {
	srcParts, err := m.resolveSrcParts(rule)
	if err != nil {
		return nil, err
	}
	var cmds []string
	for _, src := range srcParts {
		var sb strings.Builder
		fmt.Fprintf(&sb, "iptables-nft -t nat -%s POSTROUTING", action)
		if src != "" {
			sb.WriteByte(' ')
			sb.WriteString(src)
		}
		fmt.Fprintf(&sb, " -o %s", rule.OutInterface)
		if rule.Type == "MASQUERADE" {
			sb.WriteString(" -j MASQUERADE")
		} else {
			fmt.Fprintf(&sb, " -j SNAT --to-source %s", rule.ToSource)
		}
		cmds = append(cmds, sb.String())
	}
	return cmds, nil
}

// resolveSrcParts returns iptables source-match fragments for a rule.
// An empty string element means "any source" (no -s flag).
func (m *Manager) resolveSrcParts(rule *NatRule) ([]string, error) {
	if rule.SourceAliasID != "" {
		return m.resolveAliasSrcParts(rule.SourceAliasID)
	}
	if rule.Source != "" {
		return []string{"-s " + rule.Source}, nil
	}
	return []string{""}, nil // any source
}

// resolveAliasSrcParts expands an alias into iptables source-match fragments.
//
//	host/network → ["-s 10.0.0.1", "-s 10.0.0.2", ...]
//	ipset        → ["-m set --match-set <name> src"]
//	group        → recursive expansion of member aliases
//	port*        → not applicable for L3 NAT, treated as any source
func (m *Manager) resolveAliasSrcParts(aliasID string) ([]string, error) {
	if m.am == nil {
		return []string{""}, nil
	}
	alias, err := m.am.GetByID(aliasID)
	if err != nil {
		return nil, err
	}
	if alias == nil {
		log.Printf("nat: alias %s not found, applying no source restriction", aliasID)
		return []string{""}, nil
	}

	switch alias.Type {
	case "ipset":
		return []string{fmt.Sprintf("-m set --match-set %s src", alias.Name)}, nil

	case "host", "network":
		if len(alias.Entries) == 0 {
			return []string{""}, nil
		}
		parts := make([]string, len(alias.Entries))
		for i, e := range alias.Entries {
			parts[i] = "-s " + e
		}
		return parts, nil

	case "group":
		var parts []string
		for _, memberID := range alias.MemberIDs {
			sub, err := m.resolveAliasSrcParts(memberID)
			if err != nil {
				return nil, err
			}
			for _, p := range sub {
				if p != "" {
					parts = append(parts, p)
				}
			}
		}
		if len(parts) == 0 {
			return []string{""}, nil
		}
		return parts, nil

	default:
		// port / port-group — not applicable for L3 NAT source matching.
		log.Printf("nat: alias %s type %q not applicable for NAT source, ignoring", aliasID, alias.Type)
		return []string{""}, nil
	}
}

// applyRule adds a NAT rule to the kernel idempotently.
// Runs -C (check) before -A so existing rules are not duplicated on container restart (FIX-14).
func (m *Manager) applyRule(rule *NatRule) error {
	addCmds, err := m.buildCmds(rule, "A")
	if err != nil {
		return err
	}
	chkCmds, err := m.buildCmds(rule, "C")
	if err != nil {
		return err
	}
	for i, addCmd := range addCmds {
		// ExecSilent: iptables -C is an idempotency check — don't log (FIX-14).
		if _, cerr := util.ExecSilent(chkCmds[i]); cerr == nil {
			log.Printf("nat: applyRule (already in kernel): %s", addCmd)
			continue
		}
		log.Printf("nat: applyRule: %s", addCmd)
		if _, aerr := util.ExecDefault(addCmd); aerr != nil {
			return fmt.Errorf("iptables-nft: %w", aerr)
		}
	}
	return nil
}

// removeRule deletes a NAT rule from the kernel.
func (m *Manager) removeRule(rule *NatRule) error {
	cmds, err := m.buildCmds(rule, "D")
	if err != nil {
		return err
	}
	for _, cmd := range cmds {
		log.Printf("nat: removeRule: %s", cmd)
		if _, err := util.ExecDefault(cmd); err != nil {
			return fmt.Errorf("iptables-nft: %w", err)
		}
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// boolInt converts a bool to 0/1 for SQLite storage.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// strIf returns ifTrue when cond is true, otherwise ifFalse.
func strIf(cond bool, ifTrue, ifFalse string) string {
	if cond {
		return ifTrue
	}
	return ifFalse
}

// isIPOrCIDR reports whether s looks like a valid IPv4 address or CIDR prefix.
// Accepts only digits, dots, and a single optional '/prefix'.
func isIPOrCIDR(s string) bool {
	for _, c := range s {
		if c != '.' && c != '/' && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return len(s) > 0
}

// isIPv4Addr reports whether s looks like a bare IPv4 address (no prefix length).
func isIPv4Addr(s string) bool {
	for _, c := range s {
		if c != '.' && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return len(s) > 0
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
		panic("nat: manager not initialized — call SetInstance before Get()")
	}
	return instance
}
