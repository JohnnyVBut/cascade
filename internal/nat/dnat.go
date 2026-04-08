// dnat.go — Port Forwarding (DNAT) rules managed by NatManager.
//
// Each rule translates to three iptables-nft commands:
//
//	# Redirect inbound traffic on in_port → dest_ip:effective_port
//	iptables-nft -t nat -A PREROUTING -p <proto> --dport <in_port> \
//	    -j DNAT --to-destination <dest_ip>:<effective_port>
//
//	# Allow forwarded new+established packets to the destination
//	iptables-nft -A FORWARD -p <proto> -d <dest_ip> --dport <effective_port> \
//	    -m state --state NEW,ESTABLISHED,RELATED -j ACCEPT
//
//	# Allow return (established/related) packets from the destination
//	iptables-nft -A FORWARD -p <proto> -s <dest_ip> --sport <effective_port> \
//	    -m state --state ESTABLISHED,RELATED -j ACCEPT
//
// protocol="both" expands all three commands for tcp AND udp (6 commands total).
//
// dest_port=0 is a sentinel meaning "same as in_port"; it is expanded to the
// actual in_port value before any iptables command is constructed.
//
// Idempotency: -C (check) is run before -A for each command (FIX-14 pattern).
// Deletion uses -D with the same arguments.
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

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/util"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// DnatRule is a Port Forwarding (DNAT) rule stored in SQLite.
type DnatRule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Protocol    string `json:"protocol"`    // "tcp" | "udp" | "both"
	InInterface string `json:"inInterface"` // "" = any interface
	InPort      int    `json:"inPort"`
	DestIP      string `json:"destIP"`
	DestPort    int    `json:"destPort"` // 0 = same as InPort
	Comment     string `json:"comment"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"createdAt"`
}

// DnatRuleInput is the create/update request payload.
type DnatRuleInput struct {
	Name        string `json:"name"`
	Protocol    string `json:"protocol"`
	InInterface string `json:"inInterface"` // "" = any
	InPort      int    `json:"inPort"`
	DestIP      string `json:"destIP"`
	DestPort    int    `json:"destPort"`
	Comment     string `json:"comment"`
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

// RestoreAllDnat applies all enabled DNAT rules to the kernel.
// Called from RestoreAll() after InterfaceManager has brought up interfaces (FIX-13).
func (m *Manager) RestoreAllDnat() {
	rules, err := m.GetDnatRules()
	if err != nil {
		log.Printf("nat: RestoreAllDnat: failed to load rules: %v", err)
		return
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if err := m.applyDnatRule(&rule); err != nil {
			log.Printf("nat: RestoreAllDnat: failed to restore rule %q: %v", rule.Name, err)
		} else {
			log.Printf("nat: dnat restored rule %q (%s %d → %s:%d)",
				rule.Name, rule.Protocol, rule.InPort, rule.DestIP, rule.effectiveDest())
		}
	}
}

// ── Public API ────────────────────────────────────────────────────────────────

// GetDnatRules returns all DNAT rules ordered by created_at.
func (m *Manager) GetDnatRules() ([]DnatRule, error) {
	rows, err := db.DB().Query(`
		SELECT id, name, protocol, in_interface, in_port, dest_ip, dest_port, comment, enabled, created_at
		FROM nat_dnat_rules
		ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DnatRule{}
	for rows.Next() {
		var r DnatRule
		var enabled int
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Protocol, &r.InInterface, &r.InPort, &r.DestIP,
			&r.DestPort, &r.Comment, &enabled, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetDnatRule returns a single DNAT rule by ID, or nil if not found.
func (m *Manager) GetDnatRule(id string) (*DnatRule, error) {
	var r DnatRule
	var enabled int
	err := db.DB().QueryRow(`
		SELECT id, name, protocol, in_interface, in_port, dest_ip, dest_port, comment, enabled, created_at
		FROM nat_dnat_rules WHERE id = ?
	`, id).Scan(
		&r.ID, &r.Name, &r.Protocol, &r.InInterface, &r.InPort, &r.DestIP,
		&r.DestPort, &r.Comment, &enabled, &r.CreatedAt,
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

// AddDnatRule creates a new DNAT rule and applies it to the kernel immediately.
func (m *Manager) AddDnatRule(inp DnatRuleInput) (*DnatRule, error) {
	if err := validateDnat(inp); err != nil {
		return nil, err
	}

	rule := DnatRule{
		ID:          uuid.New().String(),
		Name:        strings.TrimSpace(inp.Name),
		Protocol:    inp.Protocol,
		InInterface: strings.TrimSpace(inp.InInterface),
		InPort:      inp.InPort,
		DestIP:      strings.TrimSpace(inp.DestIP),
		DestPort:    inp.DestPort,
		Comment:     strings.TrimSpace(inp.Comment),
		Enabled:     true,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	if err := m.applyDnatRule(&rule); err != nil {
		return nil, err
	}

	_, err := db.DB().Exec(`
		INSERT INTO nat_dnat_rules (id, name, protocol, in_interface, in_port, dest_ip, dest_port, comment, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rule.ID, rule.Name, rule.Protocol, rule.InInterface, rule.InPort, rule.DestIP,
		rule.DestPort, rule.Comment, boolInt(rule.Enabled), rule.CreatedAt,
	)
	if err != nil {
		_ = m.removeDnatRule(&rule)
		return nil, err
	}

	log.Printf("nat: dnat rule added: %q (%s %d → %s:%d)",
		rule.Name, rule.Protocol, rule.InPort, rule.DestIP, rule.effectiveDest())
	return &rule, nil
}

// UpdateDnatRule replaces an existing DNAT rule.
func (m *Manager) UpdateDnatRule(id string, inp DnatRuleInput) (*DnatRule, error) {
	old, err := m.GetDnatRule(id)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, fmt.Errorf("dnat rule not found")
	}
	if err := validateDnat(inp); err != nil {
		return nil, err
	}

	updated := DnatRule{
		ID:          old.ID,
		Name:        strings.TrimSpace(inp.Name),
		Protocol:    inp.Protocol,
		InInterface: strings.TrimSpace(inp.InInterface),
		InPort:      inp.InPort,
		DestIP:      strings.TrimSpace(inp.DestIP),
		DestPort:    inp.DestPort,
		Comment:     strings.TrimSpace(inp.Comment),
		Enabled:     old.Enabled,
		CreatedAt:   old.CreatedAt,
	}

	if old.Enabled {
		if err := m.removeDnatRule(old); err != nil {
			log.Printf("nat: UpdateDnatRule: remove old rule %q failed: %v", old.Name, err)
		}
	}
	if updated.Enabled {
		if err := m.applyDnatRule(&updated); err != nil {
			return nil, err
		}
	}

	_, err = db.DB().Exec(`
		UPDATE nat_dnat_rules
		SET name = ?, protocol = ?, in_interface = ?, in_port = ?, dest_ip = ?, dest_port = ?, comment = ?
		WHERE id = ?
	`, updated.Name, updated.Protocol, updated.InInterface, updated.InPort, updated.DestIP, updated.DestPort, updated.Comment, id)
	if err != nil {
		return nil, err
	}

	log.Printf("nat: dnat rule updated: %q", updated.Name)
	return &updated, nil
}

// DeleteDnatRule removes a DNAT rule from the kernel and the database.
func (m *Manager) DeleteDnatRule(id string) error {
	rule, err := m.GetDnatRule(id)
	if err != nil {
		return err
	}
	if rule == nil {
		return fmt.Errorf("dnat rule not found")
	}

	if rule.Enabled {
		if err := m.removeDnatRule(rule); err != nil {
			log.Printf("nat: DeleteDnatRule: kernel remove %q failed: %v", rule.Name, err)
		}
	}

	if _, err := db.DB().Exec(`DELETE FROM nat_dnat_rules WHERE id = ?`, id); err != nil {
		return err
	}

	log.Printf("nat: dnat rule deleted: %q", rule.Name)
	return nil
}

// ToggleDnatRule enables or disables a DNAT rule.
func (m *Manager) ToggleDnatRule(id string, enabled bool) (*DnatRule, error) {
	rule, err := m.GetDnatRule(id)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		return nil, fmt.Errorf("dnat rule not found")
	}

	if enabled && !rule.Enabled {
		if err := m.applyDnatRule(rule); err != nil {
			return nil, err
		}
	} else if !enabled && rule.Enabled {
		if err := m.removeDnatRule(rule); err != nil {
			return nil, err
		}
	}

	if _, err := db.DB().Exec(`UPDATE nat_dnat_rules SET enabled = ? WHERE id = ?`, boolInt(enabled), id); err != nil {
		return nil, err
	}

	rule.Enabled = enabled
	return rule, nil
}

// ── Private ───────────────────────────────────────────────────────────────────

// effectiveDest returns the effective destination port.
// dest_port=0 is a sentinel meaning "same as in_port".
func (r *DnatRule) effectiveDest() int {
	if r.DestPort == 0 {
		return r.InPort
	}
	return r.DestPort
}

// buildDnatCmds constructs iptables-nft commands for a DNAT rule.
// action: "A" (append), "D" (delete), "C" (check).
// All three command types (PREROUTING + 2× FORWARD) use the same action so
// that -C checks work correctly for idempotency in applyDnatRule (FIX-14).
// protocol="both" produces commands for tcp and udp.
func buildDnatCmds(rule *DnatRule, action string) []string {
	destPort := rule.effectiveDest()
	// Use stdlib-normalised IP to avoid any shell metacharacter injection.
	destIP := net.ParseIP(rule.DestIP).String()

	var protos []string
	if rule.Protocol == "both" {
		protos = []string{"tcp", "udp"}
	} else {
		protos = []string{rule.Protocol}
	}

	// Optional inbound interface scope (-i flag on PREROUTING).
	ifaceFlag := ""
	if rule.InInterface != "" {
		ifaceFlag = " -i " + rule.InInterface
	}

	var cmds []string
	for _, proto := range protos {
		// 1. PREROUTING DNAT (optionally scoped to a specific inbound interface)
		cmds = append(cmds, fmt.Sprintf(
			"iptables-nft -t nat -%s PREROUTING%s -p %s --dport %d -j DNAT --to-destination %s:%d",
			action, ifaceFlag, proto, rule.InPort, destIP, destPort,
		))
		// 2. FORWARD: new + established packets to dest
		cmds = append(cmds, fmt.Sprintf(
			"iptables-nft -%s FORWARD -p %s -d %s --dport %d -m state --state NEW,ESTABLISHED,RELATED -j ACCEPT",
			action, proto, destIP, destPort,
		))
		// 3. FORWARD: return packets from dest
		cmds = append(cmds, fmt.Sprintf(
			"iptables-nft -%s FORWARD -p %s -s %s --sport %d -m state --state ESTABLISHED,RELATED -j ACCEPT",
			action, proto, destIP, destPort,
		))
	}
	return cmds
}

// buildDnatDeleteCmds produces the delete (-D) variants.
// Delegates to buildDnatCmds with action "D" for consistency.
func buildDnatDeleteCmds(rule *DnatRule) []string {
	return buildDnatCmds(rule, "D")
}

// applyDnatRule adds DNAT rules to the kernel idempotently (-C before -A, FIX-14).
func (m *Manager) applyDnatRule(rule *DnatRule) error {
	addCmds := buildDnatCmds(rule, "A")
	chkCmds := buildDnatCmds(rule, "C")

	for i, addCmd := range addCmds {
		if _, err := util.ExecSilent(chkCmds[i]); err == nil {
			log.Printf("nat: applyDnatRule (already in kernel): %s", addCmd)
			continue
		}
		log.Printf("nat: applyDnatRule: %s", addCmd)
		if _, err := util.ExecDefault(addCmd); err != nil {
			return fmt.Errorf("iptables-nft: %w", err)
		}
	}
	return nil
}

// removeDnatRule deletes DNAT rules from the kernel.
func (m *Manager) removeDnatRule(rule *DnatRule) error {
	for _, cmd := range buildDnatDeleteCmds(rule) {
		log.Printf("nat: removeDnatRule: %s", cmd)
		if _, err := util.ExecDefault(cmd); err != nil {
			log.Printf("nat: removeDnatRule: %s (may already be gone): %v", cmd, err)
		}
	}
	return nil
}

// validateDnat checks DnatRuleInput for required fields and safe values.
func validateDnat(inp DnatRuleInput) error {
	if strings.TrimSpace(inp.Name) == "" {
		return fmt.Errorf("rule name is required")
	}
	if inp.Protocol != "tcp" && inp.Protocol != "udp" && inp.Protocol != "both" {
		return fmt.Errorf("protocol must be tcp, udp, or both")
	}
	// InInterface: optional; if set must be a safe identifier (letters, digits, dash, dot, underscore)
	if iface := strings.TrimSpace(inp.InInterface); iface != "" {
		for _, c := range iface {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '@') {
				return fmt.Errorf("inInterface contains invalid characters")
			}
		}
		if len(iface) > 15 {
			return fmt.Errorf("inInterface name too long (max 15 chars)")
		}
	}
	if inp.InPort < 1 || inp.InPort > 65535 {
		return fmt.Errorf("inPort must be between 1 and 65535")
	}
	if inp.DestPort < 0 || inp.DestPort > 65535 {
		return fmt.Errorf("destPort must be between 0 and 65535")
	}
	destIP := strings.TrimSpace(inp.DestIP)
	if destIP == "" {
		return fmt.Errorf("destination IP is required")
	}
	if net.ParseIP(destIP) == nil {
		return fmt.Errorf("invalid destination IP address")
	}
	return nil
}
