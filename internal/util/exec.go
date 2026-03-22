// Package util provides low-level helpers shared across all managers.
package util

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// DefaultTimeout is used by ExecDefault — covers all normal operations.
// Mirrors FIX-10: 30s timeout prevents hung wg/awg processes from accumulating.
const DefaultTimeout = 30 * time.Second

// FastTimeout is used by ExecFast — for polling commands like `awg show dump`.
// Mirrors FIX-10: 5s timeout kills a hung awg process quickly during 1s polling loop.
const FastTimeout = 5 * time.Second

// ExecError carries the original error plus stderr output separately.
// Callers that need to surface kernel error messages (FIX-15) can access Stderr:
//
//	if execErr, ok := err.(*util.ExecError); ok {
//	    detail := execErr.Stderr  // e.g. "RTNETLINK answers: Invalid argument"
//	}
type ExecError struct {
	Err    error
	Stderr string
	Cmd    string
}

func (e *ExecError) Error() string {
	s := strings.TrimSpace(e.Stderr)
	if s != "" {
		return fmt.Sprintf("%v: %s", e.Err, s)
	}
	return e.Err.Error()
}

// Exec runs cmd via bash with the given timeout.
//
// Behaviour mirrors Util.exec() from Node.js (FIX-10):
//   - Logs "$ cmd" to stdout when log=true
//   - Returns trimmed stdout on success
//   - Returns *ExecError on failure (includes stderr for FIX-15)
//   - Kills the process with SIGKILL on timeout
//   - On non-Linux (dev/CI) returns ("", nil) — no side effects
func Exec(cmd string, timeout time.Duration, log bool) (string, error) {
	if log {
		fmt.Printf("$ %s\n", cmd)
	}

	// Non-Linux: skip execution (useful for macOS development).
	// Mirrors: if (process.platform !== 'linux') return ''
	if runtime.GOOS != "linux" {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := exec.CommandContext(ctx, "bash", "-c", cmd)

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	if err != nil {
		// Distinguish timeout from other errors for clearer log messages.
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out after %v: %s", timeout, truncate(cmd, 80))
		}
		return "", &ExecError{
			Err:    err,
			Stderr: stderr.String(),
			Cmd:    cmd,
		}
	}

	return strings.TrimSpace(stdout.String()), nil
}

// ExecDefault runs cmd with 30s timeout, logged.
// Use for all normal operations (interface up/down, iptables, ip route add, etc.).
func ExecDefault(cmd string) (string, error) {
	return Exec(cmd, DefaultTimeout, true)
}

// ExecFast runs cmd with 5s timeout, logged.
// Use for status polling commands: `awg show dump`, `wg show dump` (FIX-10).
func ExecFast(cmd string) (string, error) {
	return Exec(cmd, FastTimeout, true)
}

// ExecSilent runs cmd with 30s timeout, NOT logged.
// Use for idempotency checks: `iptables -C` (FIX-14), `ipset test`, etc.
func ExecSilent(cmd string) (string, error) {
	return Exec(cmd, DefaultTimeout, false)
}

// ExecSilentFast runs cmd with 5s timeout, NOT logged.
// Use for silent fast checks.
func ExecSilentFast(cmd string) (string, error) {
	return Exec(cmd, FastTimeout, false)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
