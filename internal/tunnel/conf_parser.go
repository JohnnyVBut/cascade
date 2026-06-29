package tunnel

// conf_parser.go — parses a WireGuard / AmneziaWG .conf file.
//
// Supports both client configs (one [Peer] = upstream server) and server
// configs (multiple [Peer] sections = connected clients).
//
// Example client config:
//
//	[Interface]
//	PrivateKey = <base64>
//	Address    = 10.8.0.5/24
//	DNS        = 1.1.1.1
//
//	[Peer]
//	PublicKey           = <base64>
//	Endpoint            = vpn.example.com:51820
//	AllowedIPs          = 0.0.0.0/0, ::/0
//	PersistentKeepalive = 25
//
// Example server config:
//
//	[Interface]
//	PrivateKey = <base64>
//	Address    = 10.8.0.1/24
//	ListenPort = 51820
//
//	[Peer]
//	PublicKey  = <client1 pubkey>
//	AllowedIPs = 10.8.0.2/32
//
// AmneziaWG extensions (Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5) are parsed
// from the [Interface] section and used to set Protocol = amneziawg-2.0.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

// ParsedPeer holds data from a single [Peer] section.
type ParsedPeer struct {
	PublicKey    string
	PresharedKey string
	Endpoint     string
	AllowedIPs   string
	Keepalive    int
}

// ParsedConf holds the result of parsing a WireGuard .conf file.
type ParsedConf struct {
	// From [Interface]
	PrivateKey string
	Address    string // raw value, e.g. "10.8.0.5/24"
	ListenPort int    // 0 = not specified
	DNS        string // first DNS entry
	MTU        int    // 0 = not specified
	Protocol   string // "wireguard-1.0" or "amneziawg-2.0"
	AWG2       *peer.AWG2Settings

	// All [Peer] sections.
	Peers []ParsedPeer

	// Convenience aliases for the first [Peer] — used by ImportConf (uplink mode).
	PeerPublicKey    string
	PeerPresharedKey string
	PeerEndpoint     string
	PeerAllowedIPs   string
	PeerKeepalive    int
}

// ParseWGConf parses a WireGuard / AmneziaWG config file.
// Returns an error if PrivateKey or Address are missing.
// Does NOT require a [Peer] section — callers validate that based on mode.
func ParseWGConf(content string) (*ParsedConf, error) {
	c := &ParsedConf{Protocol: "wireguard-1.0"}
	awg := &peer.AWG2Settings{}
	hasAWG := false

	var section string
	var cur *ParsedPeer // current [Peer] being parsed

	flush := func() {
		if cur != nil && cur.PublicKey != "" {
			c.Peers = append(c.Peers, *cur)
		}
		cur = nil
	}

	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)

		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			next := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if next == "peer" {
				flush()
				cur = &ParsedPeer{}
			}
			section = next
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
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
				if c.Address == "" {
					c.Address = val
				}
			case "listenport":
				if n, err := strconv.Atoi(val); err == nil && n > 0 {
					c.ListenPort = n
				}
			case "dns":
				if c.DNS == "" {
					// Take only the first entry (comma-separated).
					c.DNS = strings.TrimSpace(strings.SplitN(val, ",", 2)[0])
				}
			case "mtu":
				if n, err := strconv.Atoi(val); err == nil && n > 0 {
					c.MTU = n
				}
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
			if cur == nil {
				continue
			}
			switch strings.ToLower(key) {
			case "publickey":
				cur.PublicKey = val
			case "presharedkey":
				cur.PresharedKey = val
			case "endpoint":
				cur.Endpoint = val
			case "allowedips":
				if cur.AllowedIPs == "" {
					cur.AllowedIPs = val
				} else {
					cur.AllowedIPs += ", " + val
				}
			case "persistentkeepalive":
				if n, err := strconv.Atoi(val); err == nil {
					cur.Keepalive = n
				}
			}
		}
	}
	flush()

	if c.PrivateKey == "" {
		return nil, fmt.Errorf("missing PrivateKey in [Interface] section")
	}
	if c.Address == "" {
		return nil, fmt.Errorf("missing Address in [Interface] section")
	}

	if hasAWG {
		c.Protocol = "amneziawg-2.0"
		c.AWG2 = awg
	}

	// Populate flat first-peer fields for backward compatibility.
	if len(c.Peers) > 0 {
		c.PeerPublicKey = c.Peers[0].PublicKey
		c.PeerPresharedKey = c.Peers[0].PresharedKey
		c.PeerEndpoint = c.Peers[0].Endpoint
		c.PeerAllowedIPs = c.Peers[0].AllowedIPs
		c.PeerKeepalive = c.Peers[0].Keepalive
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
