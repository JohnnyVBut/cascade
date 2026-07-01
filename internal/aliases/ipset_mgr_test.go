package aliases

import (
	"os"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/ipset"
)

// newTestAliasManager creates an aliases.Manager backed by a temporary DB and
// ipset.Manager for use in unit tests.
func newTestAliasManager(t *testing.T) *Manager {
	t.Helper()
	dir, err := os.MkdirTemp("", "cascade-aliases-test-*")
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
	im, err := ipset.New(dir)
	if err != nil {
		t.Fatalf("ipset.New: %v", err)
	}
	return New(im)
}

// TestIpsetMgr_ReturnsNonNil verifies that IpsetMgr() returns the same
// ipset.Manager that was passed to New().
func TestIpsetMgr_ReturnsNonNil(t *testing.T) {
	m := newTestAliasManager(t)
	im := m.IpsetMgr()
	if im == nil {
		t.Fatal("IpsetMgr() returned nil; expected a non-nil *ipset.Manager")
	}
}

// TestIpsetMgr_SameInstance verifies that the returned manager is the exact
// instance that was provided at construction time.
func TestIpsetMgr_SameInstance(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-aliases-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	im, err := ipset.New(dir)
	if err != nil {
		t.Fatalf("ipset.New: %v", err)
	}
	mgr := &Manager{ipsetMgr: im}
	if mgr.IpsetMgr() != im {
		t.Error("IpsetMgr() returned a different instance than the one passed to New()")
	}
}
