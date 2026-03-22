// Package settings — public IP resolver.
//
// ResolvePublicIP() tries a list of external services (Russia-friendly order),
// falls back to `ip route get 8.8.8.8` for the local outbound interface IP.
// Results are cached for 5 minutes to avoid hammering external services.
package settings

import (
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ipCacheEntry holds one cached resolution result.
type ipCacheEntry struct {
	mu        sync.Mutex
	ip        string
	warning   string
	fetchedAt time.Time
	ttl       time.Duration
}

var globalIPCache = &ipCacheEntry{ttl: 5 * time.Minute}

// ResolvePublicIP resolves the router's public IP according to mode:
//   - "manual" → returns publicIPManual as-is, no network calls
//   - "auto" (or empty) → tries external services, falls back to ip route
//
// Returns (ip, warning). warning is non-empty if the IP appears private or
// could not be determined.
func ResolvePublicIP(mode, manual string) (ip, warning string) {
	if mode == "manual" {
		if manual == "" {
			return "", "manual mode but no IP configured"
		}
		return manual, ""
	}
	return globalIPCache.resolve()
}

// InvalidateIPCache clears the cached public IP (called after mode/manual changes).
func InvalidateIPCache() {
	globalIPCache.mu.Lock()
	globalIPCache.ip = ""
	globalIPCache.fetchedAt = time.Time{}
	globalIPCache.mu.Unlock()
}

func (c *ipCacheEntry) resolve() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ip != "" && time.Since(c.fetchedAt) < c.ttl {
		return c.ip, c.warning
	}

	ip, warn := doResolveAuto()
	c.ip = ip
	c.warning = warn
	c.fetchedAt = time.Now()
	return ip, warn
}

// externalServices is tried in order; Russia-friendly services first.
var externalServices = []string{
	"https://ip.sb",
	"https://ifconfig.me/ip",
	"https://api.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://api.my-ip.io/ip",
}

var pubIPClient = &http.Client{Timeout: 3 * time.Second}

func doResolveAuto() (ip, warning string) {
	// Try external services first.
	for _, url := range externalServices {
		got := fetchIPFromURL(url)
		if got == "" {
			continue
		}
		parsed := net.ParseIP(got)
		if parsed == nil {
			continue
		}
		if isPrivateIP(parsed) {
			continue // external service returned a private IP — skip
		}
		return got, ""
	}

	// Fallback: ip route get 8.8.8.8 → local outbound interface src IP.
	got := routeGetSrc()
	if got == "" {
		return "", "could not determine public IP — all services unreachable"
	}
	if isPrivateIP(net.ParseIP(got)) {
		return got, "private address detected — router may be behind NAT"
	}
	return got, ""
}

func fetchIPFromURL(url string) string {
	resp, err := pubIPClient.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func routeGetSrc() string {
	out, err := exec.Command("ip", "route", "get", "8.8.8.8").Output()
	if err != nil {
		return ""
	}
	// Output: "8.8.8.8 via 1.2.3.1 dev eth0 src 1.2.3.4 uid 0\n    cache"
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "src" && i+1 < len(fields) {
			ip := fields[i+1]
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	return ""
}

// privateRanges is initialised once at startup.
var privateRanges []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"100.64.0.0/10", // Shared address space (RFC 6598) — carrier-grade NAT
		"fc00::/7",
		"::1/128",
	} {
		_, n, err := net.ParseCIDR(cidr)
		if err == nil {
			privateRanges = append(privateRanges, n)
		}
	}
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	for _, n := range privateRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
