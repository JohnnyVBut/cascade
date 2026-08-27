// Regression test for issue #102's non-regression half at the HTTP level:
// POST /api/tunnel-interfaces/:id/peers/import-json (importPeerJSON, the
// Cascade↔Cascade S2S JSON-import flow) must still set PeerInput.AutoGeneratePSK
// when wiring up the peer.PeerInput it builds, so AddPeer auto-generates a PSK
// when the export JSON didn't include one — unaffected by this fix, which
// only stopped the *other* caller (tunnel.ImportConf, third-party .conf
// uplinks) from doing the same.
//
// Reuses the importCfgApp/wgIfaceID/wgBinPath fixtures from TestMain in
// import_client_configs_test.go (same package, same test binary; see that
// file's comment on why the Manager singleton must be seeded up front).
package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

// TestImportPeerJSON_NoPresharedKeyInBody_AutoGeneratesPSK confirms the HTTP
// handler's AutoGeneratePSK wiring end-to-end: a JSON body with no
// "presharedKey" field results in a persisted peer that DOES have one,
// because importPeerJSON sets AutoGeneratePSK: true. The response body never
// exposes PresharedKey (sanitizePeer strips it — by design, see peers.go),
// so this asserts against the DB record via peer.GetPeer, not the response.
//
// Skipped when "wg" isn't on PATH: peer.GeneratePSK shells out to "wg
// genpsk", and util.Exec no-ops on non-Linux regardless of PATH — without a
// real binary the assertion would be meaningless rather than false (same
// reasoning as internal/tunnel/import_conf_psk_test.go's matching skip).
func TestImportPeerJSON_NoPresharedKeyInBody_AutoGeneratesPSK(t *testing.T) {
	if wgBinPath == "" {
		t.Skip("wg binary not found in PATH — skipping")
	}

	body := map[string]any{
		"name":      "remote-cascade-http",
		"publicKey": "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD=",
		"endpoint":  "remote.example.com:51820",
		"address":   "10.255.255.9/32",
		// presharedKey intentionally omitted, mimicking the first importer in
		// an S2S exchange where the remote side hasn't generated one yet.
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/tunnel-interfaces/"+wgIfaceID+"/peers/import-json", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")

	resp, err := importCfgApp.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	var decoded struct {
		Peer struct {
			ID           string `json:"id"`
			PeerType     string `json:"peerType"`
			PresharedKey string `json:"presharedKey"`
		} `json:"peer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.Peer.PeerType != "interconnect" {
		t.Errorf("peerType = %q, want interconnect", decoded.Peer.PeerType)
	}
	if decoded.Peer.PresharedKey != "" {
		t.Errorf("response peer.presharedKey = %q, want empty — sanitizePeer must strip it from JSON responses", decoded.Peer.PresharedKey)
	}
	if decoded.Peer.ID == "" {
		t.Fatal("response peer.id is empty")
	}

	stored, err := peer.GetPeer(decoded.Peer.ID)
	if err != nil {
		t.Fatalf("peer.GetPeer: %v", err)
	}
	if stored == nil {
		t.Fatal("peer.GetPeer returned nil")
	}
	if stored.PresharedKey == "" {
		t.Error("stored PresharedKey is empty, want an auto-generated PSK " +
			"(importPeerJSON must set AutoGeneratePSK: true)")
	}
}
