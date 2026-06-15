// client_groups.go — Client Group alias management.
//
// A "client-group" is a special alias type backed by a kernel ipset.
// Unlike regular ipset aliases, the ipset contents are managed automatically:
//   - On peer create/update(group change)/delete → RebuildGroupIPSet()
//   - On container restart → RestoreAllGroupIPSets()
//
// The "default" group always exists and cannot be deleted.
// Deleting a non-default group moves its peers to the default group.
package aliases

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/google/uuid"
)

const DefaultGroupName = "default"

// EnsureDefaultGroup creates the "default" client group if it doesn't exist.
// Returns the default group ID. Safe to call multiple times (idempotent).
func (m *Manager) EnsureDefaultGroup() (string, error) {
	existing, err := m.GetByName(DefaultGroupName)
	if err != nil {
		return "", fmt.Errorf("lookup default group: %w", err)
	}
	if existing != nil && existing.Type == "client-group" {
		return existing.ID, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	a := Alias{
		ID:          uuid.NewString(),
		Name:        DefaultGroupName,
		Description: "Default client group",
		Type:        "client-group",
		Entries:     []string{},
		MemberIDs:   []string{},
		IPSetName:   ipsetNameFromAlias(DefaultGroupName),
		EntryCount:  0,
		CreatedAt:   now,
	}
	// Create kernel ipset (idempotent — -exist flag).
	if err := m.ipsetMgr.CreateSet(a.IPSetName); err != nil {
		log.Printf("aliases: create default group ipset: %v (continuing)", err)
	}
	if err := insertAlias(&a); err != nil {
		return "", fmt.Errorf("insert default client group: %w", err)
	}
	log.Printf("aliases: created default client group (%s)", a.ID)
	return a.ID, nil
}

// GetDefaultGroupID returns the ID of the "default" client group, creating it if needed.
func (m *Manager) GetDefaultGroupID() (string, error) {
	return m.EnsureDefaultGroup()
}

// GetClientGroups returns all aliases of type "client-group", ordered by name.
func (m *Manager) GetClientGroups() ([]Alias, error) {
	rows, err := db.DB().Query(`
		SELECT id, name, description, type, entries, member_ids, ipset_name,
		       entry_count, generator_opts, last_updated, created_at, rate_down, rate_up
		FROM aliases WHERE type = 'client-group' ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query client groups: %w", err)
	}
	defer rows.Close()

	var out []Alias
	for rows.Next() {
		a, err := scanAliasRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	if out == nil {
		out = []Alias{}
	}
	return out, rows.Err()
}

// RebuildGroupIPSet rebuilds the kernel ipset for the given client group
// by querying all client peers currently assigned to that group.
// Updates entryCount in the aliases table.
func (m *Manager) RebuildGroupIPSet(groupID string) error {
	a, err := m.getOrNotFound(groupID)
	if err != nil {
		return err
	}
	if a.Type != "client-group" {
		return fmt.Errorf("alias %s is not a client-group", groupID)
	}

	// Query all client peers in this group — extract bare IP from "10.x.x.x/32".
	rows, err := db.DB().Query(`
		SELECT allowed_ips FROM peers
		WHERE group_id = ? AND peer_type = 'client'
	`, groupID)
	if err != nil {
		return fmt.Errorf("query peers for group %s: %w", groupID, err)
	}
	defer rows.Close()

	var ips []string
	for rows.Next() {
		var allowedIPs string
		if err := rows.Scan(&allowedIPs); err != nil {
			continue
		}
		ip := strings.TrimSpace(strings.Split(allowedIPs, "/")[0])
		if ip != "" && ip != "0.0.0.0" {
			ips = append(ips, ip)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Atomically replace ipset contents.
	if _, err := m.ipsetMgr.LoadEntries(a.IPSetName, ips); err != nil {
		return fmt.Errorf("load entries into ipset %s: %w", a.IPSetName, err)
	}

	// Update entryCount in DB.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.DB().Exec(
		`UPDATE aliases SET entry_count = ?, last_updated = ? WHERE id = ?`,
		len(ips), now, groupID,
	); err != nil {
		log.Printf("aliases: update entryCount for group %s: %v", groupID, err)
	}

	return nil
}

// RestoreAllGroupIPSets recreates all client-group ipsets on startup.
// Kernel ipsets do not survive reboots, so we rebuild them from the peers table.
// Called after InterfaceManager init (interfaces must exist for ipset rules to apply).
func (m *Manager) RestoreAllGroupIPSets() error {
	groups, err := m.GetClientGroups()
	if err != nil {
		return fmt.Errorf("list client groups: %w", err)
	}
	for _, g := range groups {
		if err := m.ipsetMgr.CreateSet(g.IPSetName); err != nil {
			log.Printf("aliases: restore: create ipset %s: %v (continuing)", g.IPSetName, err)
		}
		if err := m.RebuildGroupIPSet(g.ID); err != nil {
			log.Printf("aliases: restore: rebuild %s (%s): %v", g.Name, g.ID, err)
		}
	}
	return nil
}

// DeleteClientGroup deletes a client group, moving its peers to the default group first.
// The "default" group cannot be deleted.
// Returns the number of peers moved.
func (m *Manager) DeleteClientGroup(id string) (movedCount int, err error) {
	a, err := m.getOrNotFound(id)
	if err != nil {
		return 0, err
	}
	if a.Type != "client-group" {
		return 0, fmt.Errorf("alias %s is not a client-group", id)
	}
	if strings.EqualFold(a.Name, DefaultGroupName) {
		return 0, fmt.Errorf("the default client group cannot be deleted")
	}

	defaultID, err := m.GetDefaultGroupID()
	if err != nil {
		return 0, fmt.Errorf("get default group: %w", err)
	}

	// Move peers to default.
	res, err := db.DB().Exec(
		`UPDATE peers SET group_id = ? WHERE group_id = ?`, defaultID, id,
	)
	if err != nil {
		return 0, fmt.Errorf("move peers to default: %w", err)
	}
	n, _ := res.RowsAffected()
	movedCount = int(n)

	// Rebuild default group ipset (now has additional peers).
	if movedCount > 0 {
		if err := m.RebuildGroupIPSet(defaultID); err != nil {
			log.Printf("aliases: rebuild default after group delete: %v", err)
		}
	}

	// Destroy kernel ipset.
	if a.IPSetName != "" {
		if err := m.ipsetMgr.DestroySet(a.IPSetName); err != nil {
			log.Printf("aliases: destroy ipset %s: %v (continuing)", a.IPSetName, err)
		}
	}

	// Remove alias record.
	if _, err := db.DB().Exec(`DELETE FROM aliases WHERE id = ?`, id); err != nil {
		return movedCount, fmt.Errorf("delete client group: %w", err)
	}

	log.Printf("aliases: deleted client group %q (%s), moved %d peers to default", a.Name, id, movedCount)
	return movedCount, nil
}

// AssignPeerToDefaultGroup sets group_id = defaultGroupID for all peers where group_id = ''.
// Called once at startup to migrate existing peers created before client groups were introduced.
func (m *Manager) AssignPeerToDefaultGroup() error {
	defaultID, err := m.GetDefaultGroupID()
	if err != nil {
		return err
	}
	res, err := db.DB().Exec(
		`UPDATE peers SET group_id = ? WHERE group_id = '' AND peer_type = 'client'`, defaultID,
	)
	if err != nil {
		return fmt.Errorf("assign existing peers to default group: %w", err)
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		log.Printf("aliases: assigned %d existing client peers to default group", n)
	}
	return nil
}
