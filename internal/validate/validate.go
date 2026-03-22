// Package validate provides input validation helpers used before shell commands
// and database writes to prevent command injection and data corruption.
package validate

import (
	"encoding/base64"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// reIfaceID matches valid WireGuard interface IDs: wg10, wg11, awg0, etc.
// Only alphanumeric + underscore, 1–15 chars — safe for shell substitution.
var reIfaceID = regexp.MustCompile(`^[a-zA-Z0-9_]{1,15}$`)

// IfaceID returns an error if id is not a safe interface identifier.
func IfaceID(id string) error {
	if !reIfaceID.MatchString(id) {
		return fmt.Errorf("invalid interface id %q: must match ^[a-zA-Z0-9_]{1,15}$", id)
	}
	return nil
}

// WGKey validates a WireGuard public key or preshared key.
// Both are 32-byte values encoded as standard base64 (44 chars, trailing =).
func WGKey(key string) error {
	if key == "" {
		return fmt.Errorf("empty WireGuard key")
	}
	b, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("invalid WireGuard key (not base64): %w", err)
	}
	if len(b) != 32 {
		return fmt.Errorf("invalid WireGuard key length: got %d bytes, want 32", len(b))
	}
	return nil
}

// CIDR validates a CIDR address (e.g. "10.0.0.1/24", "0.0.0.0/0").
// Accepts multiple comma-separated CIDRs (as used in AllowedIPs).
func CIDR(cidr string) error {
	if cidr == "" {
		return nil
	}
	parts := strings.Split(cidr, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if _, _, err := net.ParseCIDR(p); err != nil {
			return fmt.Errorf("invalid CIDR %q: %w", p, err)
		}
	}
	return nil
}

// Endpoint validates a WireGuard endpoint (host:port or ip:port).
func Endpoint(ep string) error {
	if ep == "" {
		return nil
	}
	host, portStr, err := net.SplitHostPort(ep)
	if err != nil {
		return fmt.Errorf("invalid endpoint %q: %w", ep, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid endpoint port in %q", ep)
	}
	// host must be a valid IP or hostname — not contain shell metacharacters
	if strings.ContainsAny(host, ";|&`$(){}\\<>") {
		return fmt.Errorf("invalid endpoint host %q: contains shell metacharacters", host)
	}
	return nil
}
