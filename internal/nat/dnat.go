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
	ID             string `json:"id"`
	Name           string `json:"name"`
	Protocol       string `json:"protocol"`       // "tcp" | "udp" | "both"
	InInterface    string `json:"inInterface"`    // "" = any interface
	InPort         int    `json:"inPort"`
	Dest           string `json:"dest"`           // user input: IP or FQDN
	DestIP         string `json:"destIP"`         // resolved IP used in iptables
	DestPort       int    `json:"destPort"`       // 0 = same as InPort
	DestResolvedAt string `json:"destResolvedAt"` // RFC3339 of last DNS resolution; "" for plain IP
	Masquerade     bool   `json:"masquerade"`     // add POSTROUTING MASQUERADE for forwarded traffic
	Comment        string `json:"comment"`
	Enabled        bool   `json:"enabled"`
	CreatedAt      string `json:"createdAt"`
}

// DnatRuleInput is the create/update request payload.
type DnatRuleInput struct {
	Name        string `json:"name"`
	Protocol    string `json:"protocol"`
	InInterface string `json:"inInterface"` // "" = any
	InPort      int    `json:"inPort"`
	Dest        string `json:"dest"` // IP or FQDN
	DestPort    int    `json:"destPort"`
	Masquerade  bool   `json:"masquerade"`
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
		// For FQDN rules: re-resolve on startup — IP may have changed since last run.
		if rule.Dest != rule.DestIP {
			newIP, resolvedAt, err := resolveDestIP(rule.Dest)
			if err != nil {
				log.Printf("nat: RestoreAllDnat: cannot resolve %q for rule %q: %v — skipping", rule.Dest, rule.Name, err)
				continue
			}
			if newIP != rule.DestIP {
				log.Printf("nat: RestoreAllDnat: %q IP updated %s → %s", rule.Dest, rule.DestIP, newIP)
			}
			rule.DestIP = newIP
			rule.DestResolvedAt = resolvedAt
			if _, err := db.DB().Exec(
				`UPDATE nat_dnat_rules SET dest_ip = ?, dest_resolved_at = ? WHERE id = ?`,
				newIP, resolvedAt, rule.ID,
			); err != nil {
				log.Printf("nat: RestoreAllDnat: DB update for %q: %v", rule.Name, err)
			}
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
		SELECT id, name, protocol, in_interface, in_port, dest, dest_ip, dest_port, dest_resolved_at, masquerade, comment, enabled, created_at
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
		var enabled, masq int
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Protocol, &r.InInterface, &r.InPort, &r.Dest, &r.DestIP,
			&r.DestPort, &r.DestResolvedAt, &masq, &r.Comment, &enabled, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		r.Masquerade = masq != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetDnatRule returns a single DNAT rule by ID, or nil if not found.
func (m *Manager) GetDnatRule(id string) (*DnatRule, error) {
	var r DnatRule
	var enabled, masq int
	err := db.DB().QueryRow(`
		SELECT id, name, protocol, in_interface, in_port, dest, dest_ip, dest_port, dest_resolved_at, masquerade, comment, enabled, created_at
		FROM nat_dnat_rules WHERE id = ?
	`, id).Scan(
		&r.ID, &r.Name, &r.Protocol, &r.InInterface, &r.InPort, &r.Dest, &r.DestIP,
		&r.DestPort, &r.DestResolvedAt, &masq, &r.Comment, &enabled, &r.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.Enabled = enabled != 0
	r.Masquerade = masq != 0
	return &r, nil
}

// AddDnatRule creates a new DNAT rule and applies it to the kernel immediately.
func (m *Manager) AddDnatRule(inp DnatRuleInput) (*DnatRule, error) {
	if err := validateDnat(inp); err != nil {
		return nil, err
	}
	dest := strings.TrimSpace(inp.Dest)
	resolvedIP, resolvedAt, err := resolveDestIP(dest)
	if err != nil {
		return nil, err
	}

	rule := DnatRule{
		ID:             uuid.New().String(),
		Name:           strings.TrimSpace(inp.Name),
		Protocol:       inp.Protocol,
		InInterface:    strings.TrimSpace(inp.InInterface),
		InPort:         inp.InPort,
		Dest:           dest,
		DestIP:         resolvedIP,
		DestPort:       inp.DestPort,
		DestResolvedAt: resolvedAt,
		Masquerade:     inp.Masquerade,
		Comment:        strings.TrimSpace(inp.Comment),
		Enabled:        true,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}

	if err := m.applyDnatRule(&rule); err != nil {
		return nil, err
	}

	_, err = db.DB().Exec(`
		INSERT INTO nat_dnat_rules (id, name, protocol, in_interface, in_port, dest, dest_ip, dest_port, dest_resolved_at, masquerade, comment, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rule.ID, rule.Name, rule.Protocol, rule.InInterface, rule.InPort,
		rule.Dest, rule.DestIP, rule.DestPort, rule.DestResolvedAt,
		boolInt(rule.Masquerade), rule.Comment, boolInt(rule.Enabled), rule.CreatedAt,
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
	dest := strings.TrimSpace(inp.Dest)
	resolvedIP, resolvedAt, err := resolveDestIP(dest)
	if err != nil {
		return nil, err
	}

	updated := DnatRule{
		ID:             old.ID,
		Name:           strings.TrimSpace(inp.Name),
		Protocol:       inp.Protocol,
		InInterface:    strings.TrimSpace(inp.InInterface),
		InPort:         inp.InPort,
		Dest:           dest,
		DestIP:         resolvedIP,
		DestPort:       inp.DestPort,
		DestResolvedAt: resolvedAt,
		Masquerade:     inp.Masquerade,
		Comment:        strings.TrimSpace(inp.Comment),
		Enabled:        old.Enabled,
		CreatedAt:      old.CreatedAt,
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
		SET name = ?, protocol = ?, in_interface = ?, in_port = ?, dest = ?, dest_ip = ?, dest_port = ?, dest_resolved_at = ?, masquerade = ?, comment = ?
		WHERE id = ?
	`, updated.Name, updated.Protocol, updated.InInterface, updated.InPort,
		updated.Dest, updated.DestIP, updated.DestPort, updated.DestResolvedAt,
		boolInt(updated.Masquerade), updated.Comment, id)
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
		// For FQDN rules: re-resolve before applying so we use the latest IP.
		if rule.Dest != rule.DestIP {
			newIP, resolvedAt, err := resolveDestIP(rule.Dest)
			if err != nil {
				return nil, err
			}
			rule.DestIP = newIP
			rule.DestResolvedAt = resolvedAt
			if _, err := db.DB().Exec(
				`UPDATE nat_dnat_rules SET dest_ip = ?, dest_resolved_at = ? WHERE id = ?`,
				newIP, resolvedAt, rule.ID,
			); err != nil {
				log.Printf("nat: ToggleDnatRule: DB update resolved IP for %q: %v", rule.Name, err)
			}
		}
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
		// 4. POSTROUTING MASQUERADE — rewrite source to this server's IP so the
		//    destination replies back here (not directly to the original client).
		//    Required when the destination is a public server that has no route
		//    back through this machine. Skip only when Masquerade=false (e.g.
		//    hub-and-spoke WG topology where dest already routes replies here).
		if rule.Masquerade {
			cmds = append(cmds, fmt.Sprintf(
				"iptables-nft -t nat -%s POSTROUTING -p %s -d %s --dport %d -j MASQUERADE",
				action, proto, destIP, destPort,
			))
		}
	}
	return cmds
}

// buildDnatDeleteCmds produces the delete (-D) variants.
// Delegates to buildDnatCmds with action "D" for consistency.
func buildDnatDeleteCmds(rule *DnatRule) []string {
	return buildDnatCmds(rule, "D")
}

// applyDnatRule adds DNAT rules to the kernel idempotently (-C before -A, FIX-14).
// After applying, flushes conntrack entries for the inbound port so that existing
// flows (which may have been tracked without NAT) pick up the new DNAT rule.
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
	flushConntrackForDnat(rule)
	return nil
}

// removeDnatRule deletes DNAT rules from the kernel.
// After removal, flushes conntrack entries so stale NAT state is cleared.
func (m *Manager) removeDnatRule(rule *DnatRule) error {
	for _, cmd := range buildDnatDeleteCmds(rule) {
		log.Printf("nat: removeDnatRule: %s", cmd)
		if _, err := util.ExecDefault(cmd); err != nil {
			log.Printf("nat: removeDnatRule: %s (may already be gone): %v", cmd, err)
		}
	}
	flushConntrackForDnat(rule)
	return nil
}

// flushConntrackForDnat removes conntrack entries for the inbound port of a DNAT rule.
// This is required because conntrack caches NAT decisions per-flow: if a flow was
// established before the DNAT rule was added, conntrack would apply the old "no-NAT"
// decision to every subsequent packet, bypassing the nat table entirely.
// Uses conntrack(8) if available; silently skips if not installed (FIX-GO-24).
func flushConntrackForDnat(rule *DnatRule) {
	protos := []string{rule.Protocol}
	if rule.Protocol == "both" {
		protos = []string{"tcp", "udp"}
	}
	for _, proto := range protos {
		cmd := fmt.Sprintf("conntrack -D -p %s --dport %d", proto, rule.InPort)
		out, err := util.ExecSilent(cmd)
		if err != nil {
			// conntrack exits 1 with "0 flow entries have been deleted" when no entries matched —
			// this is expected on startup or after a fresh conntrack table. Not an error.
			if strings.Contains(out, "0 flow entries") {
				log.Printf("nat: flushConntrackForDnat: no entries for %s dport=%d (ok)", proto, rule.InPort)
			} else {
				log.Printf("nat: flushConntrackForDnat: %s: %v %s", cmd, err, out)
			}
		} else {
			log.Printf("nat: flushConntrackForDnat: flushed conntrack %s dport=%d", proto, rule.InPort)
		}
	}
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
	dest := strings.TrimSpace(inp.Dest)
	if dest == "" {
		return fmt.Errorf("destination is required (IP or FQDN)")
	}
	// Accept plain IP or a valid FQDN (resolved later in AddDnatRule/UpdateDnatRule).
	if net.ParseIP(dest) == nil {
		// Validate as FQDN: labels separated by dots, each label is alphanumeric + hyphens.
		for _, label := range strings.Split(dest, ".") {
			if label == "" {
				return fmt.Errorf("invalid destination: %q", dest)
			}
			for _, c := range label {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
					(c >= '0' && c <= '9') || c == '-') {
					return fmt.Errorf("invalid destination: %q (invalid character %q)", dest, string(c))
				}
			}
		}
	}
	return nil
}

// resolveDestIP resolves dest (IP or FQDN) to an IP string and a resolution timestamp.
// For plain IPs: returns the IP as-is with an empty timestamp (no DNS involved).
// For FQDNs: performs a DNS lookup; prefers the first IPv4 address.
// Returns an error if the FQDN cannot be resolved — caller must not apply the rule.
func resolveDestIP(dest string) (ip, resolvedAt string, err error) {
	if net.ParseIP(dest) != nil {
		return dest, "", nil // plain IP — no resolution needed
	}
	addrs, err := net.LookupHost(dest)
	if err != nil || len(addrs) == 0 {
		return "", "", fmt.Errorf("cannot resolve %q: %w", dest, err)
	}
	// Prefer IPv4.
	resolved := addrs[0]
	for _, a := range addrs {
		if net.ParseIP(a) != nil && net.ParseIP(a).To4() != nil {
			resolved = a
			break
		}
	}
	return resolved, time.Now().UTC().Format(time.RFC3339), nil
}
