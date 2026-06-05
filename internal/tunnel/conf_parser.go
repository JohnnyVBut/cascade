package tunnel

// conf_parser.go — parses a WireGuard / AmneziaWG client .conf file.
//
// Supports standard WireGuard format:
//
//	[Interface]
//	PrivateKey = <base64>
//	Address    = 10.8.0.5/24
//	DNS        = 1.1.1.1
//
//	[Peer]
//	PublicKey           = <base64>
//	PresharedKey        = <base64>
//	Endpoint            = vpn.example.com:51820
//	AllowedIPs          = 0.0.0.0/0, ::/0
//	PersistentKeepalive = 25
//
// AmneziaWG extensions (Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5) are parsed
// from the [Interface] section and used to set Protocol = amneziawg-2.0.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

// ParsedConf holds the result of parsing a WireGuard client .conf file.
type ParsedConf struct {
	// From [Interface]
	PrivateKey string
	Address    string // raw value, e.g. "10.8.0.5/24"
	MTU        int    // 0 = not specified
	Protocol   string // "wireguard-1.0" or "amneziawg-2.0"
	AWG2       *peer.AWG2Settings

	// From [Peer] (first peer section only)
	PeerPublicKey    string
	PeerPresharedKey string
	PeerEndpoint     string
	PeerAllowedIPs   string
	PeerKeepalive    int
}

// ParseWGConf parses a WireGuard / AmneziaWG client config file.
// Returns an error if required fields (PrivateKey, [Peer] PublicKey) are missing.
func ParseWGConf(content string) (*ParsedConf, error) {
	c := &ParsedConf{Protocol: "wireguard-1.0"}
	awg := &peer.AWG2Settings{}
	hasAWG := false

	var section string
	peerDone := false // we only parse the first [Peer] section

	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Section header.
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			next := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			// If we're entering a second [Peer] section, mark the first as done.
			if next == "peer" && c.PeerPublicKey != "" {
				peerDone = true
			}
			section = next
			continue
		}

		// Key = Value.
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		// Strip inline comments (e.g. "0.0.0.0/0 # all traffic" → "0.0.0.0/0").
		// wg-quick reference implementation does the same.
		rawVal := parts[1]
		if idx := strings.Index(rawVal, "#"); idx >= 0 {
			rawVal = rawVal[:idx]
		}
		val := strings.TrimSpace(rawVal)

		switch section {
		case "interface":
			switch strings.ToLower(key) {
			case "privatekey":
				c.PrivateKey = val
			case "address":
				c.Address = val
			case "mtu":
				if n, err := strconv.Atoi(val); err == nil && n > 0 {
					c.MTU = n
				}
			// AWG2 params — presence of any one of these marks protocol as amneziawg-2.0.
			case "jc":
				if n, err := strconv.Atoi(val); err == nil {
					awg.Jc = n
					hasAWG = true
				}
			case "jmin":
				if n, err := strconv.Atoi(val); err == nil {
					awg.Jmin = n
				}
			case "jmax":
				if n, err := strconv.Atoi(val); err == nil {
					awg.Jmax = n
				}
			case "s1":
				if n, err := strconv.Atoi(val); err == nil {
					awg.S1 = n
					hasAWG = true
				}
			case "s2":
				if n, err := strconv.Atoi(val); err == nil {
					awg.S2 = n
				}
			case "s3":
				if n, err := strconv.Atoi(val); err == nil {
					awg.S3 = n
				}
			case "s4":
				if n, err := strconv.Atoi(val); err == nil {
					awg.S4 = n
				}
			case "h1":
				awg.H1 = val
				hasAWG = true
			case "h2":
				awg.H2 = val
			case "h3":
				awg.H3 = val
			case "h4":
				awg.H4 = val
			case "i1":
				awg.I1 = val
				hasAWG = true
			case "i2":
				awg.I2 = val
			case "i3":
				awg.I3 = val
			case "i4":
				awg.I4 = val
			case "i5":
				awg.I5 = val
			}

		case "peer":
			if peerDone {
				continue // ignore additional [Peer] sections
			}
			switch strings.ToLower(key) {
			case "publickey":
				c.PeerPublicKey = val
			case "presharedkey":
				c.PeerPresharedKey = val
			case "endpoint":
				c.PeerEndpoint = val
			case "allowedips":
				// Accumulate multiple AllowedIPs lines (some generators emit one per line).
				if c.PeerAllowedIPs == "" {
					c.PeerAllowedIPs = val
				} else {
					c.PeerAllowedIPs += ", " + val
				}
			case "persistentkeepalive":
				if n, err := strconv.Atoi(val); err == nil {
					c.PeerKeepalive = n
				}
			}
		}
	}

	// Validate required fields.
	if c.PrivateKey == "" {
		return nil, fmt.Errorf("missing PrivateKey in [Interface] section")
	}
	if c.Address == "" {
		return nil, fmt.Errorf("missing Address in [Interface] section")
	}
	if c.PeerPublicKey == "" {
		return nil, fmt.Errorf("missing PublicKey in [Peer] section")
	}

	if hasAWG {
		c.Protocol = "amneziawg-2.0"
		c.AWG2 = awg
	}

	return c, nil
}

// AddressToHost32 takes a CIDR address (e.g. "10.8.0.5/24") and returns
// the host address with a /32 mask (e.g. "10.8.0.5/32").
// If the input is already /32 or has no mask, it is returned as-is (with /32 appended).
// This avoids subnet routing conflicts when the imported address overlaps with
// an existing interface on the server.
func AddressToHost32(addr string) string {
	ip := strings.SplitN(addr, "/", 2)[0]
	return ip + "/32"
}
