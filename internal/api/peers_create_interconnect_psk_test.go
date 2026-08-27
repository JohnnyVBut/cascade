// Regression test for a gap the code-reviewer caught in the issue #102 fix:
// POST /api/tunnel-interfaces/:id/peers (createPeer) is ALSO a legitimate
// Cascade-authored S2S interconnect creation path — the "Add Peer" UI's
// manual mode for peerType=interconnect has no PSK field at all, so admins
// creating an S2S peer this way relied on AddPeer's (now-gated) PSK
// auto-generation exactly like importPeerJSON does. The original fix only
// set PeerInput.AutoGeneratePSK in importPeerJSON, silently leaving
// createPeer's interconnect peers with no PSK — reintroducing the same class
// of bug issue #102 fixed, via a different call site. createPeer now sets
// AutoGeneratePSK: true for peerType=interconnect (mirroring importPeerJSON).
//
// Reuses the importCfgApp/wgIfaceID/wgBinPath fixtures from TestMain in
// import_client_configs_test.go (same package, same test binary).
package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

// TestCreatePeer_ManualInterconnect_NoPresharedKeyInBody_AutoGeneratesPSK
// mirrors TestImportPeerJSON_NoPresharedKeyInBody_AutoGeneratesPSK for the
// other Cascade-authored S2S creation path. Skipped when "wg" isn't on
// PATH — see that test's doc comment for why.
func TestCreatePeer_ManualInterconnect_NoPresharedKeyInBody_AutoGeneratesPSK(t *testing.T) {
	if wgBinPath == "" {
		t.Skip("wg binary not found in PATH — skipping")
	}

	body := map[string]any{
		"name":       "manual-s2s-peer",
		"peerType":   "interconnect",
		"publicKey":  "EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE=",
		"endpoint":   "remote2.example.com:51820",
		"allowedIPs": "10.255.255.10/32",
		// presharedKey intentionally omitted — the manual "Add Peer" UI for
		// interconnect peers has no PSK field at all.
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/tunnel-interfaces/"+wgIfaceID+"/peers", bytes.NewReader(buf))
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
			ID       string `json:"id"`
			PeerType string `json:"peerType"`
		} `json:"peer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.Peer.PeerType != "interconnect" {
		t.Errorf("peerType = %q, want interconnect", decoded.Peer.PeerType)
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
			"(createPeer must set AutoGeneratePSK: true for peerType=interconnect)")
	}
}

// Note: a matching "client peer must NOT get an auto-generated PSK" test was
// considered but dropped — createPeer's client-peer branch calls
// aliases.Get() (internal/api/peers.go's default-group assignment), and this
// test binary's TestMain (import_client_configs_test.go) never initializes
// the aliases.Manager singleton, so that path panics here regardless of this
// fix. Not worth standing up unrelated test infrastructure for: the
// AutoGeneratePSK assignment added to createPeer is syntactically scoped
// inside `if inp.PeerType == "interconnect"`, so client peers are provably
// unaffected by inspection alone.
