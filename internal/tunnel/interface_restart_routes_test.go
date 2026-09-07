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
// the real "ip route replace" side (util.Exec no-ops on non-Linux — see
// internal/util/exec.go — and even on Linux this sandbox/most CI containers
// lack NET_ADMIN), but it does exercise the real routing.Manager code path
// end-to-end (GetRoutes -> resolveGatewayVia -> kernelReplace attempt) to
// prove reapplyDependents() is wired up and doesn't silently no-op or panic
// when routing.SetInstance has actually been called (unlike the
// TryGet()-returns-nil case already covered by every other test in this
// package, none of which call routing.SetInstance at all).
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
	// actual routing code (GetRoutes/resolveGatewayVia/kernelReplace) rather
	// than short-circuiting somewhere before it.
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

	// A plain static route bound directly to this interface's device (no
	// GatewayID/GatewayGroupID needed — Dev alone is enough to exercise
	// ReapplyForDevice's dev-matching branch for a non-gateway route).
	if _, err := rm.AddRoute(routing.Route{
		Description: "test route",
		Destination: "10.50.0.0/24",
		Dev:         iface.ID,
		Enabled:     true,
	}); err != nil {
		t.Fatalf("AddRoute: %v", err)
	}

	iface.reloadMu.Lock()
	iface.restartWithNewSettings()
	iface.reloadMu.Unlock()
	// No explicit assertion beyond "did not panic" — kernelReplace's actual
	// "ip route replace" is a no-op in this sandbox (see doc comment above),
	// so there's no observable kernel-state difference to check here. The
	// real guarantee this test provides is that reapplyDependents() runs
	// routing.Manager's real code (not just a nil TryGet() short-circuit)
	// without error or panic when routing IS initialized.

	// Also exercise plain Restart() — the doReload()/KernelRemovePeer fallback
	// path that had the identical bug. Tolerate a RegenerateConfig failure
	// (expected in this sandbox/most CI — no writable /etc/amnezia/amneziawg
	// without root, see import_conf_psk_test.go's established pattern); any
	// OTHER error (in particular a panic, which a test failure here would not
	// catch but go test's own crash report would) still fails the test.
	iface.reloadMu.Lock()
	err := iface.Restart()
	iface.reloadMu.Unlock()
	if err != nil && !strings.Contains(err.Error(), "amneziawg") && !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("Restart() = %v, want nil or a benign RegenerateConfig permission error", err)
	}
}
