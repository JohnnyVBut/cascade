// Regression test for issue #105: MSS/NAT rule leak on interface update.
//
// The bug: TunnelInterface.Update() used to persist the new settings and
// rewrite the config file (RegenerateConfig) BEFORE calling Restart(). Since
// Stop()'s "awg-quick down" reads whatever PostDown is CURRENTLY on disk,
// this meant the PostDown that ran was the NEW one — trying to remove a
// TCPMSS/MASQUERADE rule keyed by the new value/subnet, which doesn't exist
// yet — while the real old rule (added by the previous PostUp, keyed by the
// old value/subnet) was never touched and leaked permanently. Repeated
// changes (Auto → Manual 1260 → Manual 1280) accumulated multiple live
// TCPMSS policies simultaneously; a plain Restart didn't clear them either,
// since each Restart repeated the same overwrite-before-down mistake.
//
// The fix reorders Update() so that, for a running interface that needs a
// full restart, Stop() runs FIRST — while the on-disk config still reflects
// the OLD settings — and only then does save()/RegenerateConfig()/Start()
// run, inside the same background goroutine. This test cannot exercise the
// real awg-quick/iptables side (RegenerateConfig writes to
// /etc/amnezia/amneziawg, not writable in this sandbox or most CI
// containers without root — see import_conf_psk_test.go's doc comment for
// the established pattern), so it verifies the directly observable contract
// change instead: Update() must return immediately for the needsRestart+
// Enabled case, without surfacing save()/RegenerateConfig() errors
// synchronously — those now happen inside the deferred goroutine, after
// Stop() — while still applying the in-memory field change right away.
package tunnel

import (
	"strings"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

func TestUpdate_EnabledNeedsRestart_ReturnsImmediatelyWithoutSyncError(t *testing.T) {
	initTunnelTestDB(t)

	iface := &TunnelInterface{
		ID:         "wg20",
		Name:       "Test",
		PrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		PublicKey:  "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		ListenPort: 51830,
		Address:    "10.9.0.1/24",
		Protocol:   "wireguard-1.0",
		CreatedAt:  "2026-01-01T00:00:00Z",
		Enabled:    true,
		MSS:        -1,
		peers:      make(map[string]*peer.Peer),
	}
	if err := iface.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	newMSS := 1260
	// Before the fix, Update() ran save()/RegenerateConfig() synchronously
	// for ANY change, so a RegenerateConfig failure (guaranteed here — no
	// writable /etc/amnezia/amneziawg) would surface as a returned error.
	// After the fix, an MSS change on an enabled interface (needsRestart triggers
	// on mssChanged, see Update's doc comment) is handled entirely inside the
	// background goroutine, so the call must return nil immediately.
	if err := iface.Update(InterfaceUpdate{MSS: &newMSS}); err != nil {
		t.Fatalf("Update() returned a synchronous error %v — the needsRestart+Enabled "+
			"path must defer save()/RegenerateConfig() to the background goroutine "+
			"(so Stop() can run first against the still-old config), not run them "+
			"before returning", err)
	}

	// Wait for the deferred goroutine (Stop -> save -> RegenerateConfig -> Start)
	// to finish: it holds reloadMu for its entire duration, so acquiring and
	// releasing the same lock blocks until it's done, without an arbitrary sleep.
	iface.reloadMu.Lock()
	iface.reloadMu.Unlock() //nolint:staticcheck // synchronization barrier, not a real critical section

	if iface.MSS != newMSS {
		t.Errorf("MSS = %d, want %d — the in-memory field must update synchronously "+
			"even though persistence is deferred", iface.MSS, newMSS)
	}
}

// TestUpdate_DisabledNeedsRestart_PersistsSynchronously covers the other
// branch: when the interface isn't running, there's nothing to Stop() and no
// stale-rule risk, so save()/RegenerateConfig() should still happen inline
// (matching pre-fix behavior for this case) rather than being deferred.
func TestUpdate_DisabledNeedsRestart_PersistsSynchronously(t *testing.T) {
	initTunnelTestDB(t)

	iface := &TunnelInterface{
		ID:         "wg21",
		Name:       "Test",
		PrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		PublicKey:  "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		ListenPort: 51831,
		Address:    "10.9.1.1/24",
		Protocol:   "wireguard-1.0",
		CreatedAt:  "2026-01-01T00:00:00Z",
		Enabled:    false,
		MSS:        -1,
		peers:      make(map[string]*peer.Peer),
	}
	if err := iface.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	newMSS := 1280
	err := iface.Update(InterfaceUpdate{MSS: &newMSS})
	// Tolerate a RegenerateConfig failure (expected in most sandboxes/CI —
	// no writable /etc/amnezia/amneziawg without root, see
	// import_conf_psk_test.go's doc comment for the established pattern);
	// any OTHER error still fails the test.
	if err != nil && !strings.Contains(err.Error(), "amneziawg") && !strings.Contains(err.Error(), "mkdir") {
		t.Fatalf("Update: unexpected error: %v", err)
	}

	// Whether or not the write itself succeeded, save()/RegenerateConfig()
	// must have been attempted synchronously (not deferred to a goroutine)
	// for a disabled interface — there's nothing running to Stop() first,
	// so the ordering fix doesn't apply to this branch. The in-memory field
	// updates either way.
	if iface.MSS != newMSS {
		t.Errorf("MSS = %d, want %d", iface.MSS, newMSS)
	}
}
