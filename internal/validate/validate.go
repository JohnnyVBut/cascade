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

// reIfaceName matches any Linux network interface name (including host interfaces
// like eth0, ens3, bond-1). Allows alphanumeric, underscore, dot, hyphen, 1–15 chars.
var reIfaceName = regexp.MustCompile(`^[a-zA-Z0-9_.\-]{1,15}$`)

// reTableName matches an iproute2 routing table name or number.
// Names: up to 31 chars, alphanumeric + underscore + hyphen (e.g. "vpn_kz", "100").
var reTableName = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,31}$`)

// IfaceID returns an error if id is not a safe interface identifier.
func IfaceID(id string) error {
	if !reIfaceID.MatchString(id) {
		return fmt.Errorf("invalid interface id %q: must match ^[a-zA-Z0-9_]{1,15}$", id)
	}
	return nil
}

// IfaceName returns an error if name is not a safe Linux network interface name.
// Unlike IfaceID, this allows hyphens and dots for host interfaces (eth0, bond-1).
func IfaceName(name string) error {
	if !reIfaceName.MatchString(name) {
		return fmt.Errorf("invalid interface name %q: must match ^[a-zA-Z0-9_.\\-]{1,15}$", name)
	}
	return nil
}

// TableName returns an error if s is not a safe iproute2 routing table name or number.
// Allows "main", "default", "local", numeric IDs, and names like "vpn_kz".
func TableName(s string) error {
	if s == "" {
		return fmt.Errorf("table name must not be empty")
	}
	if !reTableName.MatchString(s) {
		return fmt.Errorf("invalid routing table %q: must match ^[a-zA-Z0-9_\\-]{1,31}$", s)
	}
	return nil
}

// IP returns an error if s is not a valid IPv4 or IPv6 address.
func IP(s string) error {
	if net.ParseIP(s) == nil {
		return fmt.Errorf("invalid IP address %q", s)
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
