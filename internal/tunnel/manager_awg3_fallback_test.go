// Tests for Manager.buildAWG2Params — the "no matching-version default
// template → random fallback" behaviour (item 7 of the AWG3-protocol-redo
// test plan). A "3.0" fallback must enable ALL THREE Transport Protection
// toggles (HeaderProtectionKey, ContentPaddingAddition, and a timer field),
// not degrade to a plain "2.0" random profile.
package tunnel

import (
	"strings"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/awgparams"
	"github.com/JohnnyVBut/cascade/internal/peer"
)

func TestBuildAWG2Params_AWG3_NoDefaultTemplate_FullFallback(t *testing.T) {
	initTunnelTestDB(t)

	m := &Manager{}
	got, err := m.buildAWG2Params(awgparams.ProtocolAmneziaWG3)
	if err != nil {
		t.Fatalf("buildAWG2Params(amneziawg-3.0): %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil AWG2Settings")
	}
	if got.HeaderProtectionKey == "" {
		t.Error("expected HeaderProtectionKey to be set in the v3 random fallback")
	}
	if got.ContentPaddingAddition == "" {
		t.Error("expected ContentPaddingAddition to be set in the v3 random fallback")
	}
	if got.RejectAfterTime == "" {
		t.Error("expected RejectAfterTime (a timer field) to be set in the v3 random fallback")
	}
}

func TestBuildAWG2Params_AWG2_NoDefaultTemplate_NoAWG3Fields(t *testing.T) {
	initTunnelTestDB(t)

	m := &Manager{}
	got, err := m.buildAWG2Params(awgparams.ProtocolAmneziaWG2)
	if err != nil {
		t.Fatalf("buildAWG2Params(amneziawg-2.0): %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil AWG2Settings")
	}
	if got.HeaderProtectionKey != "" {
		t.Errorf("HeaderProtectionKey = %q, want empty for a 2.0 random fallback", got.HeaderProtectionKey)
	}
	if got.ContentPaddingAddition != "" {
		t.Errorf("ContentPaddingAddition = %q, want empty for a 2.0 random fallback", got.ContentPaddingAddition)
	}
	if got.RekeyAfterTime != "" || got.RekeyTimeout != "" || got.RejectAfterTime != "" ||
		got.KeepaliveTimeout != "" || got.MaxHandshakeAttempts != "" {
		t.Errorf("expected no AWG3 timer fields set for a 2.0 random fallback, got %+v", got)
	}
}

// TestCreateInterface_RejectsHeaderProtectionKeyWithSmallS3S4 is a regression
// test: amneziawg-go and the kernel module both refuse to start when
// HeaderProtectionKey is set but S3/S4 are below 12 (the cipher nonce comes
// from that padding buffer). This constraint was already enforced for
// templates (settings.CreateTemplate/UpdateTemplate) and for the interface
// create/update API handlers, but CreateInterface itself — used directly by
// ImportConf, which is fed from a parsed .conf file and bypasses those API
// handlers entirely — had no such check. That gap became reachable once
// ParseWGConf started correctly extracting HeaderProtectionKey from a real
// v3 client .conf (previously it was silently dropped, so this path could
// never receive one). See awgparams.ValidateHeaderProtectionKeyPadding.
func TestCreateInterface_RejectsHeaderProtectionKeyWithSmallS3S4(t *testing.T) {
	initTunnelTestDB(t)

	m := newTestManager()
	_, err := m.CreateInterface(CreateInput{
		Protocol: awgparams.ProtocolAmneziaWG3,
		Address:  "10.9.0.1/24",
		AWG2: &peer.AWG2Settings{
			Jc: 6, Jmin: 10, Jmax: 50,
			S1: 64, S2: 67, S3: 8, S4: 8, // below the required minimum of 12
			H1: "1-2", H2: "3-4", H3: "5-6", H4: "7-8",
			HeaderProtectionKey: "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=",
		},
	})
	if err == nil {
		t.Fatal("expected CreateInterface to reject HeaderProtectionKey with S3/S4 < 12")
	}
}

// TestCreateInterface_AcceptsHeaderProtectionKeyWithValidS3S4 confirms the
// new check isn't over-broad — valid S3/S4 (>= 12) with HeaderProtectionKey
// set must still pass the padding check and reach key generation.
//
// The padding check runs before peer.GenerateKeys, so any error after it
// must be from GenerateKeys shelling out to "awg genkey" — expected to fail
// with "command not found" on a Linux CI runner that doesn't have
// amneziawg-tools installed (unlike this repo's own macOS dev sandboxes,
// where util.Exec no-ops for every command regardless of PATH — see
// internal/util/exec.go — so this gap was invisible locally). That failure
// is unrelated to what this test asserts (the padding check itself), so
// it's tolerated the same way internal/tunnel/import_conf_psk_test.go
// tolerates its own unrelated RegenerateConfig failure.
func TestCreateInterface_AcceptsHeaderProtectionKeyWithValidS3S4(t *testing.T) {
	initTunnelTestDB(t)

	m := newTestManager()
	iface, err := m.CreateInterface(CreateInput{
		Protocol: awgparams.ProtocolAmneziaWG3,
		Address:  "10.9.0.1/24",
		AWG2: &peer.AWG2Settings{
			Jc: 6, Jmin: 10, Jmax: 50,
			S1: 64, S2: 67, S3: 12, S4: 12,
			H1: "1-2", H2: "3-4", H3: "5-6", H4: "7-8",
			HeaderProtectionKey: "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=",
		},
	})
	if err != nil && !strings.Contains(err.Error(), "genkey") {
		t.Fatalf("CreateInterface with valid S3/S4 padding: %v", err)
	}
	if err != nil {
		// GenerateKeys failed in this environment (no awg binary) before
		// CreateInterface could return an *TunnelInterface — nothing further
		// to assert on iface itself.
		return
	}
	if iface == nil {
		t.Fatal("expected non-nil interface")
	}
}
