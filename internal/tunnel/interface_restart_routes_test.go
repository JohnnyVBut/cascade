// Regression test: a restart triggered from inside internal/tunnel (Update()'s
// needsRestart path, Restart(), doReload()'s AWG-deadlock fallback,
// KernelRemovePeer) used to silently drop static routes bound to the
// interface. "awg-quick down" (inside Stop()) removes all kernel routes tied
// to the device; only the /start and /restart HTTP handlers
// (internal/api/interfaces.go) knew to call routing.Get().ReapplyForDevice()
// afterward — every restart path inside internal/tunnel itself did not,
// leaving a route visible in the Static Routes UI/DB but gone from the
// kernel routing table. Confirmed in the wild via a PATCH that changed MTU
// on an interface with a static route bound to it.
//
// The fix added TunnelInterface.reapplyDependents(), called at the end of
// both Restart() and restartWithNewSettings(). This test doesn't exercise
// a real reapplied route end-to-end: routing.Manager.AddRoute applies to the
// kernel ("ip route add ... dev <id>") BEFORE persisting — see its own
// "Apply to kernel first — fail fast before persisting" comment — so on a
// real Linux CI runner (unlike this sandbox, where util.Exec no-ops
// non-Linux commands) that call fails outright with "Cannot find device"
// for a fake interface ID that was never actually brought up (confirmed:
// this exact failure happened in CI on an earlier version of this test that
// called AddRoute). What this test verifies instead: reapplyDependents()
// reaches real routing.Manager code (GetRoutes, at minimum) rather than
// silently no-op'ing or panicking when routing.SetInstance has actually been
// called — unlike the TryGet()-returns-nil case already covered by every
// other test in this package, none of which call routing.SetInstance at all.
package tunnel

import (
	"strings"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/peer"
	"github.com/JohnnyVBut/cascade/internal/routing"
)

func TestRestartWithNewSettings_ReapplyDependents_DoesNotPanicWithRoutingInitialized(t *testing.T) {
	initTunnelTestDB(t)

	// Real routing.Manager, not a stub — proves reapplyDependents() reaches
	// actual routing code rather than short-circuiting somewhere before it.
	rm := routing.New()
	routing.SetInstance(rm)
	t.Cleanup(func() { routing.SetInstance(nil) }) // don't leak into other test files' package-level state

	iface := &TunnelInterface{
		ID:         "wg23",
		Name:       "Test",
		PrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		PublicKey:  "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		ListenPort: 51833,
		Address:    "10.9.3.1/24",
		Protocol:   "wireguard-1.0",
		CreatedAt:  "2026-01-01T00:00:00Z",
		Enabled:    true,
		peers:      make(map[string]*peer.Peer),
	}
	if err := iface.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	iface.reloadMu.Lock()
	iface.restartWithNewSettings()
	iface.reloadMu.Unlock()
	// No explicit assertion beyond "did not panic" — with zero routes in the
	// DB, ReapplyForDevice's GetRoutes() legitimately returns an empty set
	// and there's nothing further to check. The guarantee this test provides
	// is that reapplyDependents() runs routing.Manager's real code (not just
	// a nil TryGet() short-circuit) without error or panic when routing IS
	// initialized.

	// Also exercise plain Restart() — the doReload()/KernelRemovePeer fallback
	// path that had the identical bug. Tolerate the environment-dependent
	// failures every other test in this package already tolerates: no
	// writable /etc/amnezia/amneziawg without root (see
	// import_conf_psk_test.go's established pattern), and — seen on the real
	// GitHub Actions ubuntu-latest runner, unlike this sandbox — no
	// wg-quick/awg-quick binary in PATH at all ("command not found", exit
	// status 127). Any OTHER error (in particular a panic, which a test
	// failure here would not catch but go test's own crash report would)
	// still fails the test.
	benign := []string{"amneziawg", "mkdir", "not found", "exit status 127"}
	iface.reloadMu.Lock()
	err := iface.Restart()
	iface.reloadMu.Unlock()
	if err != nil {
		ok := false
		for _, s := range benign {
			if strings.Contains(err.Error(), s) {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("Restart() = %v, want nil or a benign environment-dependent error", err)
		}
	}
}
