// Regression test: globalThresholds() (called from probeICMP/probeHTTP,
// themselves run from a goroutine that Monitor.Start fires immediately and
// asynchronously) used to panic instead of falling back to defaults when the
// database wasn't initialized — because settings.GetSettings() calls
// db.DB(), which panics on a nil instance, defeating this function's own
// documented "falls back to hardcoded defaults on error" contract. This is a
// real race in production too (a probe fired right at process shutdown,
// after db.Close()), but was only actually observed via a unit test: a
// gateway created with Monitor:true fires its first probe synchronously
// inside a newly spawned goroutine, which can still be mid-flight — reaching
// this exact code path — after the test that created it returns and its
// t.Cleanup closes the test database, panicking the whole test binary
// (confirmed in CI; not reproducible locally, since this sandbox's
// util.Exec no-ops non-Linux commands, changing the timing enough to avoid
// the race in practice).
package gateway

import (
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
)

func TestGlobalThresholds_FallsBackToDefaults_WhenDBNotInitialized(t *testing.T) {
	// Deliberately do NOT call db.Init() — this package's global db instance
	// starts nil in a fresh test binary, and other tests in this package
	// that do call db.Init() also close it in t.Cleanup(), so running this
	// check first (or interleaved) exercises the real "not ready" state.
	if db.TryDB() != nil {
		t.Skip("db already initialized by another test in this run — TryDB()-nil case not exercisable here")
	}

	windowSec, healthy, degraded := globalThresholds(0)
	if windowSec != 30 || healthy != 95 || degraded != 90 {
		t.Errorf("globalThresholds() with no db = (%d, %v, %v), want hardcoded defaults (30, 95, 90)",
			windowSec, healthy, degraded)
	}
}

func TestGlobalThresholds_HonorsExplicitWindowOverride_WhenDBNotInitialized(t *testing.T) {
	if db.TryDB() != nil {
		t.Skip("db already initialized by another test in this run — TryDB()-nil case not exercisable here")
	}

	windowSec, _, _ := globalThresholds(120)
	if windowSec != 120 {
		t.Errorf("globalThresholds(120) with no db = windowSec %d, want 120 (explicit override still applies)", windowSec)
	}
}
