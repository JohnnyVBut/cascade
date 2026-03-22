// Package ipset manages kernel ipsets for firewall aliases.
// This file contains the PrefixFetcher — a port of PrefixFetcher.js.
// Fetches IPv4 prefixes from RIPEstat (country / ASN / ASN-list),
// deduplicates, normalises, and aggregates them via CIDR collapse.
package ipset

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	countryAPI = "https://stat.ripe.net/data/country-resource-list/data.json"
	asnAPI     = "https://stat.ripe.net/data/announced-prefixes/data.json"
)

var httpClient = &http.Client{Timeout: 60 * time.Second}

// fetchCountry returns aggregated IPv4 prefixes for a 2-letter country code (e.g. "RU").
func fetchCountry(country string) ([]string, error) {
	cc := strings.ToUpper(strings.TrimSpace(country))
	if len(cc) != 2 {
		return nil, fmt.Errorf("invalid country code: %q", country)
	}
	for _, c := range cc {
		if c < 'A' || c > 'Z' {
			return nil, fmt.Errorf("invalid country code: %q", country)
		}
	}

	url := fmt.Sprintf("%s?resource=%s&v4_format=prefix", countryAPI, cc)

	var resp struct {
		Data struct {
			Resources struct {
				IPv4 []string `json:"ipv4"`
			} `json:"resources"`
		} `json:"data"`
	}
	if err := fetchJSON(url, &resp); err != nil {
		return nil, err
	}
	if resp.Data.Resources.IPv4 == nil {
		return nil, fmt.Errorf("no IPv4 data for country %s", cc)
	}
	return processEntries(resp.Data.Resources.IPv4), nil
}

// fetchASN returns aggregated IPv4 prefixes announced by a single ASN (e.g. "AS12345" or "12345").
func fetchASN(asn string) ([]string, error) {
	normalized := normalizeASN(asn)
	url := fmt.Sprintf("%s?resource=%s", asnAPI, normalized)

	var resp struct {
		Data struct {
			Prefixes []struct {
				Prefix string `json:"prefix"`
			} `json:"prefixes"`
		} `json:"data"`
	}
	if err := fetchJSON(url, &resp); err != nil {
		return nil, err
	}
	if resp.Data.Prefixes == nil {
		return nil, fmt.Errorf("no prefixes data for %s", normalized)
	}

	entries := make([]string, 0, len(resp.Data.Prefixes))
	for _, p := range resp.Data.Prefixes {
		if p.Prefix != "" && !strings.Contains(p.Prefix, ":") { // IPv4 only
			entries = append(entries, p.Prefix)
		}
	}
	return processEntries(entries), nil
}

// fetchASNList fetches and merges prefixes for a comma-separated list of ASNs.
func fetchASNList(asnList string) ([]string, error) {
	parts := strings.Split(asnList, ",")
	var all []string
	for _, a := range parts {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		entries, err := fetchASN(a)
		if err != nil {
			return nil, fmt.Errorf("ASN %s: %w", a, err)
		}
		all = append(all, entries...)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("ASN list produced no results")
	}
	return deduplicateAndCollapse(all), nil
}

// writeToFile writes prefixes to a file, one CIDR per line.
func writeToFile(prefixes []string, filePath string) error {
	content := strings.Join(prefixes, "\n")
	if len(prefixes) > 0 {
		content += "\n"
	}
	return os.WriteFile(filePath, []byte(content), 0644)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// normalizeASN ensures the ASN string has an "AS" prefix.
func normalizeASN(asn string) string {
	s := strings.ToUpper(strings.TrimSpace(asn))
	if !strings.HasPrefix(s, "AS") {
		return "AS" + s
	}
	return s
}

// processEntries normalises, expands IP ranges, deduplicates and collapses CIDRs.
func processEntries(entries []string) []string {
	var cidrs []string
	for _, entry := range entries {
		e := strings.TrimSpace(entry)
		if e == "" {
			continue
		}
		if strings.Contains(e, "-") {
			cidrs = append(cidrs, rangeToSidrs(e)...)
		} else if strings.Contains(e, "/") {
			if n := normalizeCidr(e); n != "" {
				cidrs = append(cidrs, n)
			}
		}
	}
	return deduplicateAndCollapse(cidrs)
}

// deduplicateAndCollapse removes duplicate CIDRs and aggregates contiguous blocks.
func deduplicateAndCollapse(cidrs []string) []string {
	seen := make(map[string]struct{}, len(cidrs))
	unique := cidrs[:0:0]
	for _, c := range cidrs {
		if c == "" {
			continue
		}
		if _, exists := seen[c]; !exists {
			seen[c] = struct{}{}
			unique = append(unique, c)
		}
	}
	return collapseAddresses(unique)
}

// netBlock holds a parsed IPv4 prefix as (network address uint32, prefix length).
type netBlock struct {
	net uint32
	pfx int
}

// collapseAddresses aggregates CIDRs — port of PrefixFetcher._collapseAddresses().
// Algorithm:
//  1. Parse all CIDRs into (net uint32, pfx int) pairs.
//  2. Sort by (net, pfx ascending) — larger blocks first within the same start.
//  3. Remove subnets contained within an already-seen larger block.
//  4. Repeatedly merge adjacent sibling blocks (same prefix length, aligned) until stable.
func collapseAddresses(cidrs []string) []string {
	if len(cidrs) == 0 {
		return nil
	}

	// Parse
	blocks := make([]netBlock, 0, len(cidrs))
	for _, cidr := range cidrs {
		slash := strings.IndexByte(cidr, '/')
		if slash == -1 {
			continue
		}
		num, ok := ipToNum(cidr[:slash])
		if !ok {
			continue
		}
		var pfx int
		if _, err := fmt.Sscanf(cidr[slash+1:], "%d", &pfx); err != nil || pfx < 0 || pfx > 32 {
			continue
		}
		blocks = append(blocks, netBlock{net: num, pfx: pfx})
	}

	// Sort by (net ASC, pfx ASC) — larger blocks (/8) come before smaller (/24) at same address
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].net != blocks[j].net {
			return blocks[i].net < blocks[j].net
		}
		return blocks[i].pfx < blocks[j].pfx
	})

	// Remove subnets contained within a previous larger block
	deoverlapped := blocks[:0:0]
	for _, n := range blocks {
		if len(deoverlapped) == 0 {
			deoverlapped = append(deoverlapped, n)
			continue
		}
		prev := deoverlapped[len(deoverlapped)-1]
		prevEnd := uint64(prev.net) + blockSize64(prev.pfx) - 1
		nEnd := uint64(n.net) + blockSize64(n.pfx) - 1
		if uint64(n.net) >= uint64(prev.net) && nEnd <= prevEnd {
			continue // n is contained within prev — skip
		}
		deoverlapped = append(deoverlapped, n)
	}

	// Repeatedly merge adjacent sibling blocks until no more merges possible
	working := deoverlapped
	for {
		result := make([]netBlock, 0, len(working))
		merged := false
		i := 0
		for i < len(working) {
			if i+1 < len(working) {
				a, b := working[i], working[i+1]
				if a.pfx == b.pfx && a.pfx > 0 {
					size64 := blockSize64(a.pfx) // uint64 — safe for pfx=1 where size=2^31
					// Siblings: a is aligned on a 2×size boundary AND b immediately follows a
					if uint64(a.net)%(size64*2) == 0 && uint64(a.net)+size64 == uint64(b.net) {
						result = append(result, netBlock{net: a.net, pfx: a.pfx - 1})
						i += 2
						merged = true
						continue
					}
				}
			}
			result = append(result, working[i])
			i++
		}
		working = result
		if !merged {
			break
		}
	}

	out := make([]string, 0, len(working))
	for _, b := range working {
		out = append(out, fmt.Sprintf("%s/%d", numToIP(b.net), b.pfx))
	}
	return out
}

// blockSize64 returns 2^(32-pfx) as uint64 (safe for pfx=0 which gives 2^32).
func blockSize64(pfx int) uint64 {
	if pfx >= 32 {
		return 1
	}
	if pfx <= 0 {
		return 1 << 32
	}
	return 1 << (32 - pfx)
}

// normalizeCidr validates and normalises a CIDR string (masks host bits, rejects IPv6).
// Returns "" if invalid.
func normalizeCidr(cidr string) string {
	slash := strings.IndexByte(cidr, '/')
	if slash == -1 {
		return ""
	}
	ip := cidr[:slash]
	if strings.Contains(ip, ":") {
		return "" // skip IPv6
	}
	var pfx int
	if _, err := fmt.Sscanf(cidr[slash+1:], "%d", &pfx); err != nil || pfx < 0 || pfx > 32 {
		return ""
	}
	num, ok := ipToNum(ip)
	if !ok {
		return ""
	}
	var mask uint32
	if pfx > 0 {
		mask = ^uint32(0) << (32 - pfx)
	}
	return fmt.Sprintf("%s/%d", numToIP(num&mask), pfx)
}

// rangeToSidrs converts "a.b.c.d-e.f.g.h" to the minimal set of CIDRs covering the range.
// Port of PrefixFetcher._rangeToSidrs() + _summarizeRange().
func rangeToSidrs(rangeStr string) []string {
	parts := strings.SplitN(rangeStr, "-", 2)
	if len(parts) != 2 {
		return nil
	}
	startNum, ok1 := ipToNum(strings.TrimSpace(parts[0]))
	endNum, ok2 := ipToNum(strings.TrimSpace(parts[1]))
	if !ok1 || !ok2 || startNum > endNum {
		return nil
	}

	var cidrs []string
	cur := startNum
	for cur <= endNum {
		pfx := 32
		for pfx > 0 {
			candidate := pfx - 1
			if candidate == 0 {
				break // never use /0
			}
			size := uint32(1) << (32 - candidate)
			mask := ^uint32(0) << (32 - candidate)
			// Block must be aligned at cur AND fit entirely within [cur, endNum]
			if cur&mask == cur && cur+size-1 <= endNum {
				pfx = candidate
			} else {
				break
			}
		}
		size := uint32(1) << (32 - pfx)
		cidrs = append(cidrs, fmt.Sprintf("%s/%d", numToIP(cur), pfx))
		next := cur + size
		if next < cur {
			break // uint32 overflow — we've covered 255.255.255.x, done
		}
		cur = next
	}
	return cidrs
}

// ipToNum converts a dotted-decimal IPv4 string to a uint32.
func ipToNum(ip string) (uint32, bool) {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return 0, false
	}
	var num uint32
	for _, p := range parts {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil || n < 0 || n > 255 {
			return 0, false
		}
		num = (num << 8) | uint32(n)
	}
	return num, true
}

// numToIP converts a uint32 to a dotted-decimal IPv4 string.
func numToIP(num uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(num>>24)&0xFF,
		(num>>16)&0xFF,
		(num>>8)&0xFF,
		num&0xFF,
	)
}

// fetchJSON performs an HTTPS GET request and decodes the JSON response body into v.
func fetchJSON(url string, v any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "cascade/3.0 (PrefixFetcher)")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		return fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response from %s: %w", url, err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("invalid JSON from %s: %w", url, err)
	}
	return nil
}
