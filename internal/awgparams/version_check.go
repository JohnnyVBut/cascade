package awgparams

import (
	"regexp"
	"sync"

	"github.com/JohnnyVBut/cascade/internal/util"
)

// versionPattern extracts the leading "major.minor" component from a
// version string such as "amneziawg-tools v3.0.20260730 - https://..." or
// modinfo's "version:        3.0.0-1" line. The date/patch suffix is
// ignored — CLI/kernel-module compatibility breaks are tied to the
// major.minor line (see the compatibility note in the Dockerfile: CLI
// v1.0.x against kernel module 3.0.x fails with EINVAL on H1-H4, while CLI
// v3.0.x against the same kernel module works), not to exact date/patch.
var versionPattern = regexp.MustCompile(`v?(\d+\.\d+)`)

// ParseVersionMajorMinor extracts "major.minor" from raw command output.
// Returns "" if no version-like token is found.
func ParseVersionMajorMinor(output string) string {
	m := versionPattern.FindStringSubmatch(output)
	if m == nil {
		return ""
	}
	return m[1]
}

// DetectCLIVersion returns the installed awg-tools CLI's major.minor
// version (e.g. "3.0"), or "" if the awg binary is missing or its output
// couldn't be parsed. Best-effort — never returns an error, since this is
// diagnostic information, not something that should block startup.
func DetectCLIVersion() string {
	out, err := util.ExecSilent("awg --version")
	if err != nil {
		return ""
	}
	return ParseVersionMajorMinor(out)
}

// DetectKernelModuleVersion returns the loaded amneziawg kernel module's
// major.minor version (e.g. "3.0"), or "" if the module isn't installed,
// modinfo isn't available, or output couldn't be parsed. Only meaningful
// when running in kernel mode — callers should skip this entirely in
// userspace mode (awgparams.IsUserspaceMode()), where there is no separate
// kernel module to compare against.
func DetectKernelModuleVersion() string {
	out, err := util.ExecSilent("modinfo -F version amneziawg")
	if err != nil {
		return ""
	}
	return ParseVersionMajorMinor(out)
}

// VersionMismatchReport holds the result of comparing the awg CLI version
// against the loaded kernel module version.
type VersionMismatchReport struct {
	CLIVersion    string // "" if undetectable
	KernelVersion string // "" if undetectable (includes userspace mode)
	Mismatch      bool   // true only when both versions were detected AND differ
}

// CheckKernelCLIVersionMismatch compares the installed awg CLI's
// major.minor version against the loaded kernel module's major.minor
// version. Only flags a mismatch when BOTH versions were successfully
// detected and they differ — an undetectable version (missing binary,
// userspace mode, modinfo unavailable) is reported as "" but never treated
// as a mismatch, since a false "mismatch" from a detection failure would be
// misleading rather than helpful.
//
// This is a heuristic warning, not a validated compatibility matrix — the
// exact CLI/kernel-module version pairing requirements aren't pinned
// upstream (see the Dockerfile's own TODO about tracking amneziawg-go
// :latest unpinned). Treat a reported mismatch as "worth checking", not as
// proof of a real incompatibility.
//
// The result is computed once per process and cached (versionMismatchOnce)
// — the installed CLI and loaded kernel module can't change without a
// container restart, so there's no reason to spawn "awg --version"/
// "modinfo" subprocesses on every call. This matters because
// getSystemInfo (internal/api/dashboard.go) calls this on every dashboard
// poll (every 30s per open admin tab).
func CheckKernelCLIVersionMismatch() VersionMismatchReport {
	versionMismatchOnce.Do(func() {
		r := VersionMismatchReport{CLIVersion: DetectCLIVersion()}
		if !IsUserspaceMode() {
			r.KernelVersion = DetectKernelModuleVersion()
			r.Mismatch = r.CLIVersion != "" && r.KernelVersion != "" && r.CLIVersion != r.KernelVersion
		}
		cachedVersionMismatch = r
	})
	return cachedVersionMismatch
}

var (
	versionMismatchOnce   sync.Once
	cachedVersionMismatch VersionMismatchReport
)

// ResetVersionMismatchCacheForTests clears the cached CheckKernelCLIVersionMismatch
// result. Test-only — exported so tests in other packages (e.g. internal/api's
// getSystemInfo tests, which exercise this indirectly) can reset the cache
// between test cases that toggle WG_QUICK_USERSPACE_IMPLEMENTATION. Production
// code should never call this — it always wants the cached value.
func ResetVersionMismatchCacheForTests() {
	versionMismatchOnce = sync.Once{}
}

// String renders a short human-readable summary, e.g. "cli=3.0 kernel=3.0"
// or "cli=3.0 kernel=1.0 (mismatch)". Empty components render as "unknown".
func (r VersionMismatchReport) String() string {
	cli := r.CLIVersion
	if cli == "" {
		cli = "unknown"
	}
	kernel := r.KernelVersion
	if kernel == "" {
		kernel = "unknown"
	}
	s := "cli=" + cli + " kernel=" + kernel
	if r.Mismatch {
		s += " (mismatch)"
	}
	return s
}
