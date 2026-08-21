// onetimelink_test.go — regression tests for GET /cnf/:token (the
// unauthenticated one-time-link config download route).
//
// Reuses the shared TestMain/DB/tunnel.Init setup from
// import_client_configs_test.go (same package, one TestMain per package).
// Creates its own peer scoped to this file so it doesn't interfere with
// fixtures used by other tests in the package.
package api

import (
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

// otlApp is a minimal app with only the /cnf/:token route registered,
// mirroring how it's wired in production (before the auth middleware, at
// the root, not under /api).
func newOTLApp() *fiber.App {
	app := fiber.New()
	RegisterOneTimeLink(app)
	return app
}

func TestGetPeerConfigByToken_ValidTokenReturnsConfigAndConsumesToken(t *testing.T) {
	p, err := peer.CreatePeer(wgIfaceID, peer.PeerInput{
		Name:       "otl-peer-a",
		PublicKey:  "otlPeerAPublicKeyxxxxxxxxxxxxxxxxxxxxxxxxxx=",
		AllowedIPs: "10.50.0.20/32",
		PrivateKey: "otl-peer-a-priv-key",
	})
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}
	token := "abcdefghij0123456789abcdefghij01"
	if _, err := mgr().UpdatePeer(wgIfaceID, p.ID, peer.PeerUpdate{OneTimeLink: &token}); err != nil {
		t.Fatalf("UpdatePeer (set token): %v", err)
	}

	app := newOTLApp()

	req := httptest.NewRequest("GET", "/cnf/"+token, nil)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Token must be consumed — a second request for the same token 404s.
	req2 := httptest.NewRequest("GET", "/cnf/"+token, nil)
	resp2, err := app.Test(req2, 5000)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	if resp2.StatusCode != 404 {
		t.Fatalf("expected second request (reused token) to 404, got %d", resp2.StatusCode)
	}

	// Peer's OneTimeLink must be cleared in the in-memory cache too.
	if got := mgr().GetPeer(wgIfaceID, p.ID); got == nil || got.OneTimeLink != "" {
		t.Errorf("expected OneTimeLink to be cleared after use, got %+v", got)
	}
}

func TestGetPeerConfigByToken_UnknownTokenReturns404(t *testing.T) {
	// Built via strings.Repeat rather than a hand-counted literal (unlike
	// TestGetPeerConfigByToken_ShortTokenReturns404WithoutLookup's deliberately-
	// short token) so this reliably exercises the "well-formed token, no
	// matching peer found" scan path, not just the length guard.
	unknownToken := strings.Repeat("z", 32)

	app := newOTLApp()
	req := httptest.NewRequest("GET", "/cnf/"+unknownToken, nil)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for unknown token, got %d", resp.StatusCode)
	}
}

func TestGetPeerConfigByToken_ShortTokenReturns404WithoutLookup(t *testing.T) {
	app := newOTLApp()
	req := httptest.NewRequest("GET", "/cnf/tooshort", nil)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for malformed token, got %d", resp.StatusCode)
	}
}

// TestGetPeerConfigByToken_ConcurrentRequestsOnlyOneSucceeds is a regression
// test for the double-consumption race: before ConsumeOneTimeLink existed,
// two concurrent requests for the same still-valid token could both pass the
// "does this peer have this token" check before either cleared it, letting a
// "one-time" link be downloaded more than once. Fires N requests for the
// same token concurrently and asserts exactly one gets 200.
func TestGetPeerConfigByToken_ConcurrentRequestsOnlyOneSucceeds(t *testing.T) {
	p, err := peer.CreatePeer(wgIfaceID, peer.PeerInput{
		Name:       "otl-peer-race",
		PublicKey:  "otlPeerRacePublicKeyyyyyyyyyyyyyyyyyyyyyyyy=",
		AllowedIPs: "10.50.0.21/32",
		PrivateKey: "otl-peer-race-priv-key",
	})
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}
	token := strings.Repeat("r", 32)
	if _, err := mgr().UpdatePeer(wgIfaceID, p.ID, peer.PeerUpdate{OneTimeLink: &token}); err != nil {
		t.Fatalf("UpdatePeer (set token): %v", err)
	}

	app := newOTLApp()

	const concurrency = 20
	var wg sync.WaitGroup
	var successCount, notFoundCount int64
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/cnf/"+token, nil)
			resp, err := app.Test(req, 5000)
			if err != nil {
				t.Errorf("request failed: %v", err)
				return
			}
			switch resp.StatusCode {
			case 200:
				atomic.AddInt64(&successCount, 1)
			case 404:
				atomic.AddInt64(&notFoundCount, 1)
			default:
				t.Errorf("unexpected status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful download among %d concurrent requests, got %d", concurrency, successCount)
	}
	if successCount+notFoundCount != concurrency {
		t.Errorf("expected all %d requests to resolve 200 or 404, got %d total", concurrency, successCount+notFoundCount)
	}
}
