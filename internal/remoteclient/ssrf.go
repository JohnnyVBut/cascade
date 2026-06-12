// ssrf.go — SSRF protection for outbound requests to remote Cascade servers.
//
// A "remote" URL is supplied by an authenticated user and the server then makes
// HTTP requests to it (Ping, and the full request proxy in internal/api). On a
// VPN router running with --network host this is a powerful pivot primitive, so
// the target must be restricted to public, routable addresses.
//
// Two layers of defence, because checking only at validation time is open to
// DNS-rebinding (TOCTOU): the name is re-resolved between validation and the
// actual connection, and an attacker can return a public IP first and an
// internal one later.
//
//  1. ValidateRemoteURL — called when a remote is added. Parses the URL,
//     enforces http/https, and rejects any host that is (or resolves to) a
//     loopback, private, link-local, unspecified, multicast or CGNAT address.
//  2. SafeDialContext — installed on every HTTP client that talks to remotes.
//     It re-checks the concrete IP at connect time and pins the dial to that
//     validated address, closing the rebinding window.
//
// Note: Go's net package does not accept libc-style encodings (decimal
// 2130706433, octal, "127.1", hex), so those never resolve to loopback through
// the Go stack. The checks below still operate on canonical netip.Addr values
// so IPv4-mapped IPv6 (::ffff:127.0.0.1) and alternative IPv6 spellings are
// also caught.
package remoteclient

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// errBlockedAddress is returned (wrapped) when a target resolves to a
// non-public address.
func errBlockedAddress(addr string) error {
	return fmt.Errorf("remote URL must point to a public address, not %s (loopback/private/link-local addresses are blocked)", addr)
}

// blockedPrefixes lists address ranges that must never be remote targets.
// These are in addition to what netip methods already catch (loopback, private,
// link-local, multicast, unspecified).
//
//   - 100.64.0.0/10  RFC 6598  — CGNAT / shared address space (not in IsPrivate)
//   - 192.0.0.0/24   RFC 6890  — IETF protocol assignments (incl. NAT64 well-known prefix gateway)
//   - 240.0.0.0/4    RFC 1112  — reserved / future use (incl. 255.255.255.255 broadcast)
//   - 2002::/16      RFC 3056  — 6to4: embeds a full IPv4 address in bits 16-47;
//     a 6to4 address can encode any IPv4 including 127.0.0.1 or 192.168.x.x
//   - 64:ff9b::/96   RFC 6052  — IPv4/IPv6 translation (NAT64 well-known prefix);
//     the low 32 bits are a raw IPv4 address
//   - 64:ff9b:1::/48 RFC 8215  — local-use NAT64 prefix; same IPv4-embedding risk
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}

// isBlockedIP reports whether addr is a non-public address that must not be a
// remote target.
func isBlockedIP(addr netip.Addr) bool {
	// Unmap IPv4-mapped IPv6 (e.g. ::ffff:127.0.0.1) to its IPv4 form so the
	// checks below see the real address.
	addr = addr.Unmap()
	if addr.IsLoopback() ||
		addr.IsPrivate() || // RFC1918 (10/8, 172.16/12, 192.168/16) + IPv6 ULA fc00::/7
		addr.IsLinkLocalUnicast() || // 169.254/16, fe80::/10
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsInterfaceLocalMulticast() ||
		addr.IsUnspecified() { // 0.0.0.0, ::
		return true
	}
	for _, pfx := range blockedPrefixes {
		if pfx.Contains(addr) {
			return true
		}
	}
	return false
}

// ValidateRemoteURL ensures raw is a valid http/https URL whose host is — or
// resolves exclusively to — public, routable addresses. If any resolved IP is
// non-public the whole URL is rejected (conservative: multi-A records are only
// accepted when every address passes).
func ValidateRemoteURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL must start with http:// or https://")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL must include a host")
	}

	// Literal IP — check directly, no DNS.
	if addr, err := netip.ParseAddr(host); err == nil {
		if isBlockedIP(addr) {
			return errBlockedAddress(addr.String())
		}
		return nil
	}

	// Hostname — resolve and check every address.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("could not resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q resolved to no addresses", host)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return errBlockedAddress(ip.String())
		}
	}
	return nil
}

// SafeDialContext is a net.Dialer.DialContext replacement that blocks
// connections to non-public addresses.
//
// It uses net.Dialer.Control so the standard library handles DNS resolution,
// happy-eyeballs (RFC 8305), and multi-A failover as usual. The Control hook
// fires for every candidate address just before connect(2) — after the kernel
// has picked the concrete IP but before the socket is connected. At that point
// the address is already an IP literal, so there is no TOCTOU window:
// the validated address is exactly what gets connected.
//
// This also covers redirect targets: if proxyClient follows a Location header,
// SafeDialContext is called again for the redirect destination and will block
// an internal address even if the original host was public.
func SafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d := net.Dialer{
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return fmt.Errorf("unexpected non-IP address in Control hook: %s", host)
			}
			if isBlockedIP(ip) {
				return errBlockedAddress(ip.String())
			}
			return nil
		},
	}
	return d.DialContext(ctx, network, addr)
}
