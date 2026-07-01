package firewall

import (
	"testing"
)

// TestFlushAll_DoesNotPanic verifies that FlushAll runs without panicking even
// when iptables-nft is not available (the commands use "|| true" so exec errors
// are suppressed by design).
func TestFlushAll_DoesNotPanic(t *testing.T) {
	mgr, _ := initTestDB(t)
	// FlushAll calls iptables-nft which will fail silently in the test environment.
	// The only invariant we check is that the function completes without panicking.
	mgr.FlushAll()
}
