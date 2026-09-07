// Package api — unit tests for the /api/dashboard/system-info handler,
// covering the AWG CLI/kernel-module version diagnostics fields added
// alongside internal/awgparams/version_check.go.
package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/awgparams"
)

// TestGetSystemInfo_UserspaceMode_AWGVersionFieldsZeroValued verifies that
// the new awgCliVersion/awgKernelVersion/awgVersionMismatch JSON fields are
// present (not omitted) and left at their zero values when running in
// userspace mode, per getSystemInfo's `if !awgparams.IsUserspaceMode()` gate.
func TestGetSystemInfo_UserspaceMode_AWGVersionFieldsZeroValued(t *testing.T) {
	awgparams.ResetVersionMismatchCacheForTests()
	if err := os.Setenv("WG_QUICK_USERSPACE_IMPLEMENTATION", "amneziawg-go"); err != nil {
		t.Fatalf("os.Setenv: %v", err)
	}
	defer os.Unsetenv("WG_QUICK_USERSPACE_IMPLEMENTATION")

	app := fiber.New()
	app.Get("/api/dashboard/system-info", getSystemInfo)

	req := httptest.NewRequest("GET", "/api/dashboard/system-info", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	body := buf.Bytes()

	// The JSON keys must be present in the raw body (not just absent because
	// the struct fields have `omitempty`) — confirms the SystemInfo struct
	// tags don't accidentally hide the fields when they're zero-valued.
	for _, key := range []string{`"awgCliVersion"`, `"awgKernelVersion"`, `"awgVersionMismatch"`} {
		if !bytes.Contains(body, []byte(key)) {
			t.Errorf("response missing key %s; body=%s", key, body)
		}
	}

	var info SystemInfo
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("json.Unmarshal: %v; body=%s", err, body)
	}
	if info.AWGCliVersion != "" {
		t.Errorf("AWGCliVersion = %q, want empty in userspace mode", info.AWGCliVersion)
	}
	if info.AWGKernelVersion != "" {
		t.Errorf("AWGKernelVersion = %q, want empty in userspace mode", info.AWGKernelVersion)
	}
	if info.AWGVersionMismatch {
		t.Error("AWGVersionMismatch = true, want false in userspace mode")
	}
}

// TestGetSystemInfo_KernelMode_NoFalseMismatch verifies that in kernel mode,
// on this non-Linux test runner (where util.Exec no-ops and both CLI/kernel
// versions come back undetectable), the handler never reports a mismatch —
// an undetected version pair must not be conflated with a real mismatch.
func TestGetSystemInfo_KernelMode_NoFalseMismatch(t *testing.T) {
	awgparams.ResetVersionMismatchCacheForTests()
	os.Unsetenv("WG_QUICK_USERSPACE_IMPLEMENTATION")

	app := fiber.New()
	app.Get("/api/dashboard/system-info", getSystemInfo)

	req := httptest.NewRequest("GET", "/api/dashboard/system-info", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var info SystemInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if info.AWGVersionMismatch {
		t.Error("AWGVersionMismatch = true, want false for undetected versions")
	}
}

// TestGetSystemInfo_ReturnsHostnameAndUptimeKeys is a minimal smoke test
// confirming the handler still returns 200 with the pre-existing fields
// alongside the new ones — guards against the new code path breaking the
// rest of the handler.
func TestGetSystemInfo_ReturnsHostnameAndUptimeKeys(t *testing.T) {
	app := fiber.New()
	app.Get("/api/dashboard/system-info", getSystemInfo)

	req := httptest.NewRequest("GET", "/api/dashboard/system-info", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	body := buf.Bytes()

	for _, key := range []string{`"hostname"`, `"uptime"`, `"memPct"`} {
		if !bytes.Contains(body, []byte(key)) {
			t.Errorf("response missing key %s; body=%s", key, body)
		}
	}
}
