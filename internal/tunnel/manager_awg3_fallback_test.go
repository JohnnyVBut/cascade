// Tests for Manager.buildAWG2Params — the "no matching-version default
// template → random fallback" behaviour (item 7 of the AWG3-protocol-redo
// test plan). A "3.0" fallback must enable ALL THREE Transport Protection
// toggles (HeaderProtectionKey, ContentPaddingAddition, and a timer field),
// not degrade to a plain "2.0" random profile.
package tunnel

import (
	"testing"

	"github.com/JohnnyVBut/cascade/internal/awgparams"
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
