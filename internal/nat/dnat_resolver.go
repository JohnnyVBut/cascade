// dnat_resolver.go — Background DNS poller for DNAT rules with FQDN destinations.
//
// Every 5 minutes, for each enabled DNAT rule where dest is an FQDN (not a plain IP):
//   1. Resolve the FQDN to an IP address.
//   2. If the IP has changed since last resolution:
//      a. Remove the stale iptables rules (old IP).
//      b. Update the rule's DestIP in memory and in SQLite.
//      c. Apply fresh iptables rules (new IP).
//      d. Log the change (future: Telegram notification hook).
//
// The resolver is started via StartDnatResolver() after RestoreAll().
package nat

import (
	"log"
	"net"
	"time"

	"github.com/JohnnyVBut/cascade/internal/db"
)

const dnatResolveInterval = 5 * time.Minute

// StartDnatResolver launches the background FQDN polling goroutine.
// Must be called after RestoreAll() so that in-kernel rules exist before we try to update them.
func (m *Manager) StartDnatResolver() {
	go func() {
		ticker := time.NewTicker(dnatResolveInterval)
		defer ticker.Stop()
		for range ticker.C {
			m.refreshDnatFQDNs()
		}
	}()
}

// refreshDnatFQDNs resolves FQDNs for all enabled DNAT rules and updates iptables + DB when IP changes.
func (m *Manager) refreshDnatFQDNs() {
	rules, err := m.GetDnatRules()
	if err != nil {
		log.Printf("nat: dnat resolver: failed to load rules: %v", err)
		return
	}

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		// Skip plain IP rules (Dest == DestIP means no FQDN involved).
		if net.ParseIP(rule.Dest) != nil {
			continue
		}

		newIP, resolvedAt, err := resolveDestIP(rule.Dest)
		if err != nil {
			log.Printf("nat: dnat resolver: %q: cannot resolve %q: %v", rule.Name, rule.Dest, err)
			continue
		}
		if newIP == rule.DestIP {
			continue // no change — nothing to do
		}

		log.Printf("nat: dnat resolver: %q: %q IP changed %s → %s — updating iptables",
			rule.Name, rule.Dest, rule.DestIP, newIP)

		// Remove stale iptables rules (old IP).
		if err := m.removeDnatRule(&rule); err != nil {
			log.Printf("nat: dnat resolver: %q: remove old iptables rules failed: %v", rule.Name, err)
		}

		// Update in-memory copy and apply new rules.
		rule.DestIP = newIP
		rule.DestResolvedAt = resolvedAt
		if err := m.applyDnatRule(&rule); err != nil {
			log.Printf("nat: dnat resolver: %q: apply new iptables rules failed: %v", rule.Name, err)
			continue
		}

		// Persist new resolved IP.
		if _, err := db.DB().Exec(
			`UPDATE nat_dnat_rules SET dest_ip = ?, dest_resolved_at = ? WHERE id = ?`,
			newIP, resolvedAt, rule.ID,
		); err != nil {
			log.Printf("nat: dnat resolver: %q: DB update failed: %v", rule.Name, err)
		}
	}
}
