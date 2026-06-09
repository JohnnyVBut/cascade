package tunnel

import (
	"log"
	"time"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

// StartExpiryChecker runs a background goroutine that disables peers whose
// expiredAt timestamp has passed. Checks every minute. Stops when stopCh closes.
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

		// Peer has expired — disable it.
		disabled := false
		upd := peer.PeerUpdate{Enabled: &disabled}
		if _, err := m.UpdatePeer(p.InterfaceID, p.ID, upd); err != nil {
			log.Printf("expiry: failed to disable expired peer %q (%s): %v", p.Name, p.ID, err)
		} else {
			log.Printf("expiry: disabled expired peer %q (%s) on interface %s", p.Name, p.ID, p.InterfaceID)
		}
	}
}
