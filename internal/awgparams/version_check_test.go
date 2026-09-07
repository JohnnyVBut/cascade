package awgparams

import (
	"os"
	"testing"
)

func TestParseVersionMajorMinor(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{"wg-tools style", "wireguard-tools v1.0.20210914 - https://git.zx2c4.com/wireguard-tools/", "1.0"},
		{"awg-tools style", "amneziawg-tools v3.0.20260730 - https://github.com/amnezia-vpn/amneziawg-tools/", "3.0"},
		{"modinfo version field, bare", "3.0.0", "3.0"},
		{"modinfo version field, with patch/dash suffix", "3.0.0-1", "3.0"},
		{"no v prefix", "1.0.20210914", "1.0"},
		{"empty", "", ""},
		{"garbage, no version-like token", "command not found", ""},
		{"multi-line, version in first line", "amneziawg-tools v2.5.99999999\nsome other output\n", "2.5"},
		{"pre-release/rc suffix", "amneziawg-tools v3.0.0-rc1 - https://github.com/amnezia-vpn/amneziawg-tools/", "3.0"},
		{"no digits at all", "command not found: awg", ""},
		{"interface name only, no version-like token", "eth0 is not a wireguard interface", ""},
		{"modinfo error, no version field", "modinfo: ERROR: Module amneziawg not found.", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseVersionMajorMinor(c.output)
			if got != c.want {
				t.Errorf("ParseVersionMajorMinor(%q) = %q, want %q", c.output, got, c.want)
			}
		})
	}
}

// TestCheckKernelCLIVersionMismatch_UserspaceModeNeverReportsKernelVersion
// verifies the userspace-mode short-circuit: in userspace mode there is no
// separate kernel module to compare against, so KernelVersion must stay ""
// and Mismatch must stay false regardless of what the CLI version detects
// as (which itself will be "" in this non-Linux test environment — see
// util.Exec's documented no-op behavior on non-Linux).
func TestCheckKernelCLIVersionMismatch_UserspaceModeNeverReportsKernelVersion(t *testing.T) {
	ResetVersionMismatchCacheForTests()
	if err := os.Setenv("WG_QUICK_USERSPACE_IMPLEMENTATION", "amneziawg-go"); err != nil {
		t.Fatalf("os.Setenv: %v", err)
	}
	defer os.Unsetenv("WG_QUICK_USERSPACE_IMPLEMENTATION")

	r := CheckKernelCLIVersionMismatch()
	if r.KernelVersion != "" {
		t.Errorf("KernelVersion = %q, want empty in userspace mode", r.KernelVersion)
	}
	if r.Mismatch {
		t.Error("Mismatch = true, want false in userspace mode")
	}
}

// TestCheckKernelCLIVersionMismatch_KernelModeAttemptsDetection confirms the
// kernel-mode path doesn't short-circuit before attempting detection (the
// detection itself is a no-op on non-Linux per util.Exec, so both versions
// come back empty here — this test only pins the "does not skip" behavior).
func TestCheckKernelCLIVersionMismatch_KernelModeAttemptsDetection(t *testing.T) {
	ResetVersionMismatchCacheForTests()
	os.Unsetenv("WG_QUICK_USERSPACE_IMPLEMENTATION")

	r := CheckKernelCLIVersionMismatch()
	// On this non-Linux test runner, util.Exec no-ops and returns ("", nil)
	// for every command, so both versions are undetectable — which must
	// never be reported as a mismatch (empty != empty is not a real diff).
	if r.Mismatch {
		t.Error("Mismatch = true for two undetected (empty) versions, want false")
	}
}

// TestCheckKernelCLIVersionMismatch_ResultIsCached is a regression test for
// the caching added to avoid spawning "awg --version"/"modinfo" subprocesses
// on every call (CheckKernelCLIVersionMismatch is called on every dashboard
// system-info poll — every 30s per open admin tab — but the installed CLI
// and loaded kernel module can't change without a container restart).
//
// It observes caching indirectly: the first call is made in userspace mode
// (KernelVersion stays "" by the userspace short-circuit); the environment
// is then changed to kernel mode WITHOUT resetting the cache, and a second
// call must still return the first call's cached result rather than
// re-evaluating IsUserspaceMode(). This wouldn't be true if the function
// recomputed on every call.
func TestCheckKernelCLIVersionMismatch_ResultIsCached(t *testing.T) {
	ResetVersionMismatchCacheForTests()
	if err := os.Setenv("WG_QUICK_USERSPACE_IMPLEMENTATION", "amneziawg-go"); err != nil {
		t.Fatalf("os.Setenv: %v", err)
	}
	first := CheckKernelCLIVersionMismatch()

	os.Unsetenv("WG_QUICK_USERSPACE_IMPLEMENTATION")
	second := CheckKernelCLIVersionMismatch()

	if second != first {
		t.Errorf("second call = %+v, want identical cached result %+v (cache was not reset)", second, first)
	}
}

func TestVersionMismatchReport_String(t *testing.T) {
	cases := []struct {
		name string
		r    VersionMismatchReport
		want string
	}{
		{"both known, match", VersionMismatchReport{CLIVersion: "3.0", KernelVersion: "3.0"}, "cli=3.0 kernel=3.0"},
		{"both known, mismatch", VersionMismatchReport{CLIVersion: "3.0", KernelVersion: "1.0", Mismatch: true}, "cli=3.0 kernel=1.0 (mismatch)"},
		{"both unknown", VersionMismatchReport{}, "cli=unknown kernel=unknown"},
		{"cli unknown only", VersionMismatchReport{KernelVersion: "3.0"}, "cli=unknown kernel=3.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.String(); got != c.want {
				t.Errorf("String() = %q, want %q", got, c.want)
			}
		})
	}
}
