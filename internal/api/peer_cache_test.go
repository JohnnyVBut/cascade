// peer_cache_test.go — regression test for a duplicate-in-memory-peer bug
// found while verifying the OTL fix (GitHub issue #99): TunnelInterface's
// UpdatePeer/ReloadPeerFromDB used to key t.peers by the caller-supplied ID
// parameter (e.g. a Fiber route param string) instead of the stored value's
// own .ID field. When that key ever drifted from the peer's real ID — the
// reporter traced this to route-param strings not being safe to retain as
// long-lived map keys — the next write created a second entry under the new
// key instead of replacing the existing one, so the peer appeared twice in
// GetAllPeers() with one stale copy (old OneTimeLink still set) and one
// fresh copy, letting a "one-time" link be reused.
//
// Reuses the shared TestMain/DB/tunnel.Init setup from
// import_client_configs_test.go (same package, one TestMain per package).
package api

import (
	"testing"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

func TestUpdatePeer_RepeatedUpdatesDoNotDuplicateInMemoryEntry(t *testing.T) {
	p, err := peer.CreatePeer(wgIfaceID, peer.PeerInput{
		Name:       "cache-peer-a",
		PublicKey:  "cachePeerAPublicKeyzzzzzzzzzzzzzzzzzzzzzzzz=",
		AllowedIPs: "10.50.0.22/32",
		PrivateKey: "cache-peer-a-priv-key",
	})
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}

	// peer.CreatePeer (DB-only, like the existing fixtures in this package)
	// doesn't populate the in-memory cache — the first UpdatePeer call below
	// is what actually caches it. Capture the baseline AFTER that first
	// caching write, then verify none of the SUBSEQUENT updates grow the map.
	firstName := "cache-peer-a-1"
	if _, err := mgr().UpdatePeer(wgIfaceID, p.ID, peer.PeerUpdate{Name: &firstName}); err != nil {
		t.Fatalf("UpdatePeer (initial cache write): %v", err)
	}
	before := mgr().GetInterface(wgIfaceID).PeerCount()

	// Repeated updates (mirrors real traffic: rename, set/clear a one-time
	// link, rename again) — the map must never grow from this, since no new
	// peer is being created.
	for i, name := range []string{"cache-peer-a-2", "cache-peer-a-3"} {
		n := name
		if _, err := mgr().UpdatePeer(wgIfaceID, p.ID, peer.PeerUpdate{Name: &n}); err != nil {
			t.Fatalf("UpdatePeer #%d: %v", i, err)
		}
	}
	if _, err := mgr().ReloadPeerFromDB(wgIfaceID, p.ID); err != nil {
		t.Fatalf("ReloadPeerFromDB: %v", err)
	}

	after := mgr().GetInterface(wgIfaceID).PeerCount()
	if after != before {
		t.Errorf("PeerCount changed from %d to %d after only updating an existing peer — map key drift likely created a duplicate entry", before, after)
	}

	// No two entries should ever share this peer's ID, and the single
	// surviving entry must reflect the last update.
	var matches int
	for _, got := range mgr().GetAllPeers() {
		if got.ID == p.ID {
			matches++
			if got.Name != "cache-peer-a-3" {
				t.Errorf("expected latest Name on the single cached entry, got %q", got.Name)
			}
		}
	}
	if matches != 1 {
		t.Errorf("expected exactly 1 in-memory entry for peer %s, found %d (duplicate map key)", p.ID, matches)
	}
}
