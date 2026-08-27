// Regression tests for issue #102: a third-party WireGuard .conf import
// (ImportConf, the Uplink S2S flow) must not auto-generate a PresharedKey
// for a peer whose source .conf had none — the remote server never learns
// that PSK, which silently breaks the handshake. See peer.PeerInput's
// AutoGeneratePSK doc comment and the matching condition in
// TunnelInterface.AddPeer.
//
// These tests call TunnelInterface.AddPeer directly with the exact
// PeerInput shape ImportConf builds (see manager.go's ImportConf,
// "Add the remote server as an interconnect peer"), rather than going
// through the full ImportConf/CreateInterface path, because
// RegenerateConfig writes to /etc/amnezia/amneziawg — not writable in this
// sandbox (or most CI containers) without root. AddPeer's PSK decision
// (the exact logic this issue is about) doesn't depend on that write
// succeeding: AddPeer returns the created, persisted peer alongside a
// "regenerate config" error rather than discarding it (see
// internal/tunnel/interface.go's `if err := t.RegenerateConfig(); err !=
// nil { return p, err }` — p is non-nil). Config generation itself is
// already covered by TestParseWGConf_* in conf_parser_test.go and the
// peer.ToWgConfig tests in internal/peer/peer_test.go.
package tunnel

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

// addPeerTolerateRegenerateConfigFailure calls AddPeer and treats a
// "regenerate config" failure as expected/benign in this sandbox (no
// writable /etc/amnezia/amneziawg) — any OTHER error still fails the test.
func addPeerTolerateRegenerateConfigFailure(t *testing.T, iface *TunnelInterface, inp peer.PeerInput) *peer.Peer {
	t.Helper()
	p, err := iface.AddPeer(inp)
	if err != nil && !strings.Contains(err.Error(), "amneziawg") && !strings.Contains(err.Error(), "mkdir") {
		t.Fatalf("AddPeer: unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("AddPeer returned a nil peer")
	}
	return p
}

func TestAddPeer_ImportConfStylePeer_NoPresharedKey_StaysEmpty(t *testing.T) {
	initTunnelTestDB(t)

	m := newTestManager()
	iface, err := m.CreateInterface(CreateInput{
		Protocol: "wireguard-1.0",
		Address:  "10.60.0.1/24",
	})
	if err != nil {
		t.Fatalf("CreateInterface: %v", err)
	}

	// Mirrors manager.go's ImportConf exactly: PeerType "interconnect",
	// PresharedKey from the parsed .conf (empty when the .conf has none),
	// AutoGeneratePSK left at its zero value (false).
	p := addPeerTolerateRegenerateConfigFailure(t, iface, peer.PeerInput{
		Name:                "upstream",
		PublicKey:           "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		PresharedKey:        "",
		Endpoint:            "warp.example.com:2408",
		AllowedIPs:          "0.0.0.0/0, ::/0",
		ClientAllowedIPs:    "0.0.0.0/0, ::/0",
		PeerType:            "interconnect",
		PersistentKeepalive: 25,
	})

	if p.PeerType != "interconnect" {
		t.Errorf("PeerType = %q, want interconnect", p.PeerType)
	}
	if p.PresharedKey != "" {
		t.Errorf("PresharedKey = %q, want empty — a third-party .conf with no "+
			"PresharedKey must not get one auto-generated (issue #102)", p.PresharedKey)
	}
}

func TestAddPeer_ImportConfStylePeer_WithPresharedKey_UsedAsIs(t *testing.T) {
	initTunnelTestDB(t)

	m := newTestManager()
	iface, err := m.CreateInterface(CreateInput{
		Protocol: "wireguard-1.0",
		Address:  "10.60.0.2/24",
	})
	if err != nil {
		t.Fatalf("CreateInterface: %v", err)
	}

	const wantPSK = "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC="
	p := addPeerTolerateRegenerateConfigFailure(t, iface, peer.PeerInput{
		Name:         "upstream",
		PublicKey:    "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		PresharedKey: wantPSK, // present in the source .conf
		Endpoint:     "vpn.example.com:51820",
		AllowedIPs:   "0.0.0.0/0",
		PeerType:     "interconnect",
	})

	if p.PresharedKey != wantPSK {
		t.Errorf("PresharedKey = %q, want %q (the value from the .conf, unmodified)",
			p.PresharedKey, wantPSK)
	}
}

// TestAddPeer_S2SImportStylePeer_AutoGeneratePSK_StillGenerates is the
// non-regression half: the Cascade↔Cascade S2S JSON-import flow
// (internal/api/peers.go's importPeerJSON) explicitly opts into
// auto-generation via AutoGeneratePSK — that behavior must be unaffected
// by this fix. Requires a real "wg genpsk" (skipped where wg isn't
// installed): util.Exec no-ops on non-Linux, so GeneratePSK would return ""
// there regardless of whether AddPeer's AutoGeneratePSK branch ran at all,
// making the assertion meaningless rather than false — see peer_test.go's
// TestDerivePublicKey_AcceptsWellFormedKeyFormat for the same pattern.
func TestAddPeer_S2SImportStylePeer_AutoGeneratePSK_StillGenerates(t *testing.T) {
	if _, err := exec.LookPath("wg"); err != nil {
		t.Skip("wg binary not found in PATH — skipping")
	}
	initTunnelTestDB(t)

	m := newTestManager()
	iface, err := m.CreateInterface(CreateInput{
		Protocol: "wireguard-1.0",
		Address:  "10.60.0.3/24",
	})
	if err != nil {
		t.Fatalf("CreateInterface: %v", err)
	}

	p := addPeerTolerateRegenerateConfigFailure(t, iface, peer.PeerInput{
		Name:            "remote-cascade",
		PublicKey:       "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		PresharedKey:    "", // remote export didn't include one yet — first importer
		AllowedIPs:      "10.255.255.2/32",
		PeerType:        "interconnect",
		AutoGeneratePSK: true, // set by importPeerJSON
	})

	if p.PresharedKey == "" {
		t.Error("PresharedKey is empty, want an auto-generated PSK for the S2S JSON-import flow")
	}
}
