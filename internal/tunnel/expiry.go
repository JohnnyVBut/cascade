package tunnel

import (
	"log"
	"time"

	"github.com/JohnnyVBut/cascade/internal/peer"
	"github.com/JohnnyVBut/cascade/internal/settings"
)

// StartExpiryChecker runs a background goroutine that handles peers whose
// expiredAt timestamp has passed according to the configured ExpiredPeerPolicy.
// Checks every minute. Stops when stopCh closes.
func StartExpiryChecker(stopCh <-chan struct{}) {
	go func() {
		// First check shortly after startup so any already-expired peers
		// from before a restart are handled quickly.
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()

		tick := time.NewTicker(60 * time.Second)
		defer tick.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-timer.C:
				checkExpiredPeers()
			case <-tick.C:
				checkExpiredPeers()
			}
		}
	}()
}

func checkExpiredPeers() {
	m := Get()
	if m == nil {
		return
	}

	s, err := settings.GetSettings()
	if err != nil {
		log.Printf("expiry: failed to load settings: %v", err)
		return
	}
	policy := s.ExpiredPeerPolicy
	if policy == "" {
		policy = "disable" // default
	}

	now := time.Now().UTC()
	allPeers := m.GetAllPeers()

	for _, p := range allPeers {
		if !p.Enabled || p.ExpiredAt == "" {
			continue
		}
		expAt, err := time.Parse(time.RFC3339, p.ExpiredAt)
		if err != nil {
			// Fallback: YYYY-MM-DD (legacy format stored before normalisation was added)
			expAt, err = time.Parse("2006-01-02", p.ExpiredAt)
			if err != nil {
				log.Printf("expiry: peer %q has unparseable expiredAt %q, skipping", p.ID, p.ExpiredAt)
				continue
			}
		}
		if now.Before(expAt) {
			continue
		}

		// Peer has expired — apply the configured policy.
		switch policy {
		case "restrict":
			applyRestrictPolicy(m, p, s)
		default: // "disable"
			enabled := false
			upd := peer.PeerUpdate{Enabled: &enabled}
			if _, err := m.UpdatePeer(p.InterfaceID, p.ID, upd); err != nil {
				log.Printf("expiry: failed to disable expired peer %q (%s): %v", p.Name, p.ID, err)
			} else {
				log.Printf("expiry: disabled expired peer %q (%s) on interface %s", p.Name, p.ID, p.InterfaceID)
			}
		}
	}
}

// applyRestrictPolicy keeps the peer enabled but applies rate limits and/or
// moves it to the configured expired-peer group. The peer's previous group is
// saved so it can be restored when the expiry date is extended.
//
// Idempotent: already-restricted peers (same rate limits + already in target group)
// are skipped to avoid repeated SQLite writes every 60 s.
func applyRestrictPolicy(m *Manager, p *peer.Peer, s *settings.GlobalSettings) {
	upd := peer.PeerUpdate{}
	changed := false
	actions := []string{}

	// Apply rate limits only if they differ from the current values — idempotency guard.
	if s.ExpiredPeerRateDown > 0 && p.RateDown != s.ExpiredPeerRateDown {
		rd := s.ExpiredPeerRateDown
		upd.RateDown = &rd
		changed = true
		actions = append(actions, "rate-down-limited")
	}
	if s.ExpiredPeerRateUp > 0 && p.RateUp != s.ExpiredPeerRateUp {
		ru := s.ExpiredPeerRateUp
		upd.RateUp = &ru
		changed = true
		actions = append(actions, "rate-up-limited")
	}

	// Move to expired-peer group if configured and not already there.
	// Save the current group into PreviousGroupId so it can be restored on renewal.
	if s.ExpiredPeerGroupId != "" && p.GroupID != s.ExpiredPeerGroupId {
		prevGroup := p.GroupID
		if prevGroup == "" {
			prevGroup = "default"
		}
		upd.PreviousGroupId = &prevGroup
		upd.GroupID = &s.ExpiredPeerGroupId
		changed = true
		actions = append(actions, "moved-to-expired-group")
	}

	// Always set PreviousGroupId when any policy action fires (even rate-limit-only).
	// It serves as a "policy was applied" marker so renewal can clear rate limits.
	// If PreviousGroupId is already set (second tick, idempotency), leave it alone.
	if changed && upd.PreviousGroupId == nil && p.PreviousGroupId == "" {
		prev := p.GroupID
		if prev == "" {
			prev = "default"
		}
		upd.PreviousGroupId = &prev
	}

	if !changed {
		// Policy is "restrict" but nothing needs updating (already applied, or nothing configured).
		if s.ExpiredPeerRateDown == 0 && s.ExpiredPeerRateUp == 0 && s.ExpiredPeerGroupId == "" {
			log.Printf("expiry: policy=restrict but no rate limits or group configured; peer %q (%s) left unchanged", p.Name, p.ID)
		}
		return
	}

	if _, err := m.UpdatePeer(p.InterfaceID, p.ID, upd); err != nil {
		log.Printf("expiry: failed to restrict expired peer %q (%s): %v", p.Name, p.ID, err)
	} else {
		log.Printf("expiry: restricted expired peer %q (%s) on interface %s: %v", p.Name, p.ID, p.InterfaceID, actions)
	}
}
