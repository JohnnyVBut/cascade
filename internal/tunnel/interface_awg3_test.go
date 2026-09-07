// Tests for the AWG 3.0 Transport Protection fields threaded through
// TunnelInterface.save() / scanInterface() (migration v41 columns).
package tunnel

import (
	"os"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/peer"
)

// initTunnelTestDB creates a fresh temp SQLite database for one test.
func initTunnelTestDB(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cascade-tunnel-awg3-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
}

func TestSaveScanInterface_AWG3FieldsRoundTrip(t *testing.T) {
	initTunnelTestDB(t)

	iface := &TunnelInterface{
		ID:         "wg10",
		Name:       "AWG3-Test",
		PrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		PublicKey:  "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		ListenPort: 51830,
		Address:    "10.8.0.1/24",
		Protocol:   "amneziawg-2.0",
		CreatedAt:  "2026-01-01T00:00:00Z",
		AWG2: &peer.AWG2Settings{
			Jc: 5, Jmin: 10, Jmax: 100,
			S1: 20, S2: 25, S3: 30, S4: 35,
			H1: "h1", H2: "h2", H3: "h3", H4: "h4",
			I1: "i1", I2: "i2", I3: "i3", I4: "i4", I5: "i5",
			HeaderProtectionKey:    "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
			ContentPaddingAddition: "0-1",
			RekeyAfterTime:         "1h-2h",
			RekeyTimeout:           "5-10",
			RejectAfterTime:        "3h",
			KeepaliveTimeout:       "15-25",
			MaxHandshakeAttempts:   "20",
			RandomTrailers:         "on",
			DisableCookies:         "on",
		},
		peers: make(map[string]*peer.Peer),
	}

	if err := iface.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := scanInterface("wg10")
	if err != nil {
		t.Fatalf("scanInterface: %v", err)
	}
	if loaded.AWG2 == nil {
		t.Fatal("loaded.AWG2 is nil, want populated AWG2Settings")
	}
	if loaded.AWG2.HeaderProtectionKey != "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=" {
		t.Errorf("HeaderProtectionKey = %q, want AWG3 test key", loaded.AWG2.HeaderProtectionKey)
	}
	if loaded.AWG2.ContentPaddingAddition != "0-1" {
		t.Errorf("ContentPaddingAddition = %q, want '0-1'", loaded.AWG2.ContentPaddingAddition)
	}
	if loaded.AWG2.RekeyAfterTime != "1h-2h" {
		t.Errorf("RekeyAfterTime = %q, want '1h-2h'", loaded.AWG2.RekeyAfterTime)
	}
	if loaded.AWG2.RekeyTimeout != "5-10" {
		t.Errorf("RekeyTimeout = %q, want '5-10'", loaded.AWG2.RekeyTimeout)
	}
	if loaded.AWG2.RejectAfterTime != "3h" {
		t.Errorf("RejectAfterTime = %q, want '3h'", loaded.AWG2.RejectAfterTime)
	}
	if loaded.AWG2.KeepaliveTimeout != "15-25" {
		t.Errorf("KeepaliveTimeout = %q, want '15-25'", loaded.AWG2.KeepaliveTimeout)
	}
	if loaded.AWG2.MaxHandshakeAttempts != "20" {
		t.Errorf("MaxHandshakeAttempts = %q, want '20'", loaded.AWG2.MaxHandshakeAttempts)
	}
	if loaded.AWG2.RandomTrailers != "on" {
		t.Errorf("RandomTrailers = %q, want 'on'", loaded.AWG2.RandomTrailers)
	}
	if loaded.AWG2.DisableCookies != "on" {
		t.Errorf("DisableCookies = %q, want 'on'", loaded.AWG2.DisableCookies)
	}
	// Existing AWG2 fields must still round-trip (regression check).
	if loaded.AWG2.Jc != 5 || loaded.AWG2.I5 != "i5" {
		t.Errorf("existing AWG2 fields regressed: Jc=%d I5=%q", loaded.AWG2.Jc, loaded.AWG2.I5)
	}
}

// TestSaveScanInterface_NilAWG2_RoundTripsWithNoAWG2Block is a regression
// check: a plain WireGuard 1.0 interface (AWG2 == nil) must still round-trip
// with AWG2 == nil after the v41 migration added the new nullable columns.
func TestSaveScanInterface_NilAWG2_RoundTripsWithNoAWG2Block(t *testing.T) {
	initTunnelTestDB(t)

	iface := &TunnelInterface{
		ID:         "wg11",
		Name:       "WG1-Test",
		PrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		PublicKey:  "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		ListenPort: 51831,
		Address:    "10.9.0.1/24",
		Protocol:   "wireguard-1.0",
		CreatedAt:  "2026-01-01T00:00:00Z",
		AWG2:       nil,
		peers:      make(map[string]*peer.Peer),
	}

	if err := iface.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := scanInterface("wg11")
	if err != nil {
		t.Fatalf("scanInterface: %v", err)
	}
	if loaded.AWG2 != nil {
		t.Errorf("loaded.AWG2 = %+v, want nil for a WireGuard 1.0 interface", loaded.AWG2)
	}
}
