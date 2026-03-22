package util

import (
	"net"
	"strconv"
	"strings"
)

// IsValidIPv4 reports whether s is a valid dotted-quad IPv4 address.
// Mirrors Util.isValidIPv4() from Node.js.
func IsValidIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

// IsValidIP reports whether s is a valid IPv4 or IPv6 address.
func IsValidIP(s string) bool {
	return net.ParseIP(s) != nil
}

// IsValidCIDR reports whether s is valid CIDR notation (e.g. "10.0.0.0/24").
func IsValidCIDR(s string) bool {
	_, _, err := net.ParseCIDR(s)
	return err == nil
}
