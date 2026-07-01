package tunnel

import (
	"testing"
)

// TestStopAll_EmptyManager verifies that StopAll does not panic when there are
// no managed interfaces.
func TestStopAll_EmptyManager(t *testing.T) {
	m := newTestManager()
	// Should not panic; Stop() on each interface will be a no-op because the map
	// is empty — nothing to iterate.
	m.StopAll()
}

// TestStopAll_NilInterface verifies that StopAll gracefully handles an interface
// that has disappeared from the map between the ID snapshot and GetInterface.
// This exercises the nil-guard inside StopAll.
func TestStopAll_NilInterfaceGuard(t *testing.T) {
	m := newTestManager("10.0.0.1/24")
	// Remove the interface after newTestManager has added it.
	// StopAll first captures IDs under RLock, then calls GetInterface per ID.
	// If the interface was removed in between, GetInterface returns nil and
	// StopAll must skip it without panicking.
	m.mu.Lock()
	delete(m.interfaces, "wg10")
	m.mu.Unlock()

	// Must not panic even though the snapshot has "wg10" but the map is now empty.
	m.StopAll()
}
