// Package peer manages WireGuard/AmneziaWG peer CRUD, config generation, and QR codes.
//
// Storage: SQLite `peers` table (migration v1 + v6).
//
// Peer model:
//   - client peer: server generated keys, downloadable config + QR code
//   - interconnect peer: manual keys (S2S tunnel), no private key stored server-side
//
// Runtime fields (transferRx/Tx, latestHandshakeAt, runtimeEndpoint) are NOT stored
// in SQLite — they come from periodic polling of `wg/awg show dump` in TunnelInterface.
//
// Key generation:
//   wg genkey                 → private key
//   echo <priv> | wg pubkey   → public key
//   wg genpsk                 → pre-shared key
// For amneziawg-2.0 use "awg" binary, which accepts the same subcommands.
//
// QR code: SVG generated from config text using rsc.io/qr (pure Go).
// The SVG is rendered cell-by-cell so it scales perfectly at any resolution.
package peer

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"rsc.io/qr"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/util"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Peer holds all persisted and runtime fields for a WireGuard peer.
type Peer struct {
	// Persisted fields (SQLite)
	ID                  string `json:"id"`
	InterfaceID         string `json:"interfaceId"`
	Name                string `json:"name"`
	PublicKey           string `json:"publicKey"`
	PrivateKey          string `json:"privateKey"`          // "" for interconnect peers
	PresharedKey        string `json:"presharedKey"`
	Endpoint            string `json:"endpoint"`            // remote endpoint host:port
	AllowedIPs          string `json:"allowedIPs"`          // hub-side routing (e.g. "10.8.0.2/32")
	Address             string `json:"address"`             // tunnel IP with iface mask ("10.8.0.2/24")
	ClientAllowedIPs    string `json:"clientAllowedIPs"`    // used in the client config [Peer] section
	PeerType            string `json:"peerType"`            // client | interconnect
	PersistentKeepalive int    `json:"persistentKeepalive"`
	Enabled             bool   `json:"enabled"`
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
	ExpiredAt           string `json:"expiredAt"`   // "" = no expiry
	OneTimeLink         string `json:"oneTimeLink"` // "" = no one-time link

	// Computed from PrivateKey (not stored separately)
	DownloadableConfig bool `json:"downloadableConfig"`

	// Persisted traffic totals (migration v11) — lifetime accumulated bytes.
	// Flushed to SQLite every 60 s and before wg-quick down.
	// Updated each poll tick by TunnelInterface.GetStatus().
	TotalRx int64 `json:"totalRx"`
	TotalTx int64 `json:"totalTx"`

	// Runtime fields — populated by TunnelInterface.GetStatus(), NOT persisted.
	// TransferRx/Tx are the raw kernel counters (reset on wg-quick down).
	// Use TotalRx/TotalTx for lifetime totals displayed in the UI.
	TransferRx        int64   `json:"transferRx"`
	TransferTx        int64   `json:"transferTx"`
	LatestHandshakeAt *string `json:"latestHandshakeAt"`
	RuntimeEndpoint   string  `json:"runtimeEndpoint"`
}

// PeerInput is the create/update request payload.
type PeerInput struct {
	Name                string `json:"name"`
	PublicKey           string `json:"publicKey"`
	PrivateKey          string `json:"privateKey"`
	PresharedKey        string `json:"presharedKey"`
	Endpoint            string `json:"endpoint"`
	AllowedIPs          string `json:"allowedIPs"`
	Address             string `json:"address"`
	ClientAllowedIPs    string `json:"clientAllowedIPs"`
	PeerType            string `json:"peerType"`
	PersistentKeepalive int    `json:"persistentKeepalive"`
	// Special flags (not stored directly)
	GenerateKeys   bool `json:"generateKeys"`   // server generates wg key pair + PSK
	AutoAllocateIP bool `json:"autoAllocateIP"` // caller sets AllowedIPs before passing here
}

// PeerUpdate contains the fields that can be changed via PATCH.
// nil pointer = do not update that field.
type PeerUpdate struct {
	Name                *string `json:"name"`
	Endpoint            *string `json:"endpoint"`
	AllowedIPs          *string `json:"allowedIPs"`
	ClientAllowedIPs    *string `json:"clientAllowedIPs"`
	PersistentKeepalive *int    `json:"persistentKeepalive"`
	Enabled             *bool   `json:"enabled"`
	ExpiredAt           *string `json:"expiredAt"`
	OneTimeLink         *string `json:"oneTimeLink"`
}

// InterfaceData carries the interface fields needed for config/QR generation.
// Defined here to avoid a circular import (tunnel → peer, peer ↛ tunnel).
type InterfaceData struct {
	ID                      string
	Name                    string
	Protocol                string // "wireguard-1.0" or "amneziawg-2.0"
	PublicKey               string
	Address                 string // CIDR e.g. "10.8.0.1/24"
	ListenPort              int
	DNS                     string // for client [Interface] section
	DefaultClientAllowedIPs string
	Host                    string // WG_HOST env (used in Endpoint line of client config)
	Settings                *AWG2Settings // nil for WireGuard 1.0
}

// AWG2Settings holds AmneziaWG 2.0 obfuscation parameters for config generation.
type AWG2Settings struct {
	Jc   int    `json:"jc"`
	Jmin int    `json:"jmin"`
	Jmax int    `json:"jmax"`
	S1   int    `json:"s1"`
	S2   int    `json:"s2"`
	S3   int    `json:"s3"`
	S4   int    `json:"s4"`
	H1   string `json:"h1"`
	H2   string `json:"h2"`
	H3   string `json:"h3"`
	H4   string `json:"h4"`
	I1   string `json:"i1"`
	I2   string `json:"i2"`
	I3   string `json:"i3"`
	I4   string `json:"i4"`
	I5   string `json:"i5"`
}

// ── CRUD ──────────────────────────────────────────────────────────────────────

// GetPeers returns all peers for an interface ordered by created_at.
func GetPeers(interfaceID string) ([]Peer, error) {
	rows, err := db.DB().Query(`
		SELECT id, interface_id, name, public_key, private_key, preshared_key,
		       endpoint, allowed_ips, address, client_allowed_ips,
		       peer_type, persistent_keepalive, enabled,
		       created_at, updated_at, expired_at, one_time_link,
		       total_rx, total_tx
		FROM peers
		WHERE interface_id = ?
		ORDER BY created_at
	`, interfaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Peer
	for rows.Next() {
		p, err := scanPeer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPeer returns a single peer by ID, or nil if not found.
func GetPeer(id string) (*Peer, error) {
	row := db.DB().QueryRow(`
		SELECT id, interface_id, name, public_key, private_key, preshared_key,
		       endpoint, allowed_ips, address, client_allowed_ips,
		       peer_type, persistent_keepalive, enabled,
		       created_at, updated_at, expired_at, one_time_link,
		       total_rx, total_tx
		FROM peers WHERE id = ?
	`, id)
	p, err := scanPeerRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// CreatePeer inserts a new peer. Key generation and IP allocation must be done
// by the caller (TunnelInterface) before calling CreatePeer.
func CreatePeer(interfaceID string, inp PeerInput) (*Peer, error) {
	if err := validatePeerInput(inp); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	p := Peer{
		ID:                  uuid.New().String(),
		InterfaceID:         interfaceID,
		Name:                strings.TrimSpace(inp.Name),
		PublicKey:           strings.TrimSpace(inp.PublicKey),
		PrivateKey:          strings.TrimSpace(inp.PrivateKey),
		PresharedKey:        strings.TrimSpace(inp.PresharedKey),
		Endpoint:            strings.TrimSpace(inp.Endpoint),
		AllowedIPs:          strings.TrimSpace(inp.AllowedIPs),
		Address:             strings.TrimSpace(inp.Address),
		ClientAllowedIPs:    inp.ClientAllowedIPs,
		PeerType:            strOr(inp.PeerType, "client"),
		PersistentKeepalive: intOr(inp.PersistentKeepalive, 25),
		Enabled:             true,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	p.DownloadableConfig = p.PrivateKey != ""

	if err := insertPeer(p); err != nil {
		return nil, err
	}

	log.Printf("peer: created %q (%s) for interface %s", p.Name, p.ID, interfaceID)
	return &p, nil
}

// UpdatePeer applies non-nil fields from upd to the peer and persists.
// Returns the updated peer. Fields not in upd are unchanged.
func UpdatePeer(id string, upd PeerUpdate) (*Peer, error) {
	p, err := GetPeer(id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("peer not found")
	}

	if upd.Name != nil {
		p.Name = strings.TrimSpace(*upd.Name)
	}
	if upd.Endpoint != nil {
		p.Endpoint = strings.TrimSpace(*upd.Endpoint)
	}
	if upd.AllowedIPs != nil {
		p.AllowedIPs = strings.TrimSpace(*upd.AllowedIPs)
	}
	if upd.ClientAllowedIPs != nil {
		p.ClientAllowedIPs = *upd.ClientAllowedIPs
	}
	if upd.PersistentKeepalive != nil {
		p.PersistentKeepalive = *upd.PersistentKeepalive
	}
	if upd.Enabled != nil {
		p.Enabled = *upd.Enabled
	}
	if upd.ExpiredAt != nil {
		p.ExpiredAt = *upd.ExpiredAt
	}
	if upd.OneTimeLink != nil {
		p.OneTimeLink = *upd.OneTimeLink
	}
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := updatePeer(*p); err != nil {
		return nil, err
	}
	return p, nil
}

// DeletePeer removes a peer from SQLite.
func DeletePeer(id string) error {
	p, err := GetPeer(id)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("peer not found")
	}
	if _, err := db.DB().Exec(`DELETE FROM peers WHERE id = ?`, id); err != nil {
		return err
	}
	log.Printf("peer: deleted %q (%s)", p.Name, id)
	return nil
}

// ── Traffic accumulation ──────────────────────────────────────────────────────

// SaveTrafficTotals persists lifetime accumulated RX/TX bytes for a peer.
// Called by TunnelInterface.FlushTrafficTotals() every 60 s and before wg-quick down.
// UPDATE on a non-existent peer is a no-op (returns nil) — safe after peer deletion.
func SaveTrafficTotals(peerID string, totalRx, totalTx int64) error {
	_, err := db.DB().Exec(
		`UPDATE peers SET total_rx = ?, total_tx = ? WHERE id = ?`,
		totalRx, totalTx, peerID,
	)
	return err
}

// ── Key generation ────────────────────────────────────────────────────────────

// GeneratedKeys holds a freshly generated WireGuard key triplet.
type GeneratedKeys struct {
	PrivateKey   string
	PublicKey    string
	PresharedKey string
}

// GeneratePSK runs wg/awg genpsk and returns a fresh pre-shared key.
// bin is "wg" for WireGuard 1.0 and "awg" for AmneziaWG 2.0.
func GeneratePSK(bin string) (string, error) {
	if bin == "" {
		bin = "wg"
	}
	psk, err := util.ExecDefault(bin + " genpsk")
	if err != nil {
		return "", fmt.Errorf("genpsk: %w", err)
	}
	return strings.TrimSpace(psk), nil
}

// GenerateKeys runs wg/awg genkey + pubkey + genpsk.
// bin is "wg" for WireGuard 1.0 and "awg" for AmneziaWG 2.0.
func GenerateKeys(bin string) (GeneratedKeys, error) {
	if bin == "" {
		bin = "wg"
	}

	priv, err := util.ExecDefault(bin + " genkey")
	if err != nil {
		return GeneratedKeys{}, fmt.Errorf("genkey: %w", err)
	}
	priv = strings.TrimSpace(priv)

	// Pass private key via stdin substitute: echo <priv> | wg pubkey
	// ExecDefault logs the command — mask private key in the log.
	pubCmd := fmt.Sprintf("echo %s | %s pubkey", priv, bin)
	pub, err := util.Exec(pubCmd, util.DefaultTimeout, false) // log=false: hides private key
	if err != nil {
		return GeneratedKeys{}, fmt.Errorf("pubkey: %w", err)
	}
	pub = strings.TrimSpace(pub)

	psk, err := util.ExecDefault(bin + " genpsk")
	if err != nil {
		return GeneratedKeys{}, fmt.Errorf("genpsk: %w", err)
	}
	psk = strings.TrimSpace(psk)

	return GeneratedKeys{PrivateKey: priv, PublicKey: pub, PresharedKey: psk}, nil
}

// ── Config generation ─────────────────────────────────────────────────────────

// ToWgConfig returns the [Peer] section for the hub-side wg-quick config.
// Returns "" for disabled peers — they are excluded from the kernel config.
func (p *Peer) ToWgConfig() string {
	if !p.Enabled {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n[Peer]\n# %s\n", p.Name)
	fmt.Fprintf(&sb, "PublicKey = %s\n", p.PublicKey)

	if p.PresharedKey != "" {
		fmt.Fprintf(&sb, "PresharedKey = %s\n", p.PresharedKey)
	}

	fmt.Fprintf(&sb, "AllowedIPs = %s\n", p.AllowedIPs)

	if p.Endpoint != "" {
		fmt.Fprintf(&sb, "Endpoint = %s\n", p.Endpoint)
	}

	if p.PersistentKeepalive > 0 {
		fmt.Fprintf(&sb, "PersistentKeepalive = %d\n", p.PersistentKeepalive)
	}

	return sb.String()
}

// GenerateRemoteConfig produces the full client config (when PrivateKey is known)
// or an instructional template (when peer was added manually without server-side key gen).
func (p *Peer) GenerateRemoteConfig(iface InterfaceData) string {
	if p.PrivateKey != "" {
		return p.generateCompleteConfig(iface)
	}
	return p.generateTemplateConfig(iface)
}

// generateCompleteConfig produces a minimal ready-to-use config suitable for QR codes.
// Keeps comment noise low — QR codes have limited capacity.
func (p *Peer) generateCompleteConfig(iface InterfaceData) string {
	var sb strings.Builder

	sb.WriteString("[Interface]\n")
	fmt.Fprintf(&sb, "PrivateKey = %s\n", p.PrivateKey)

	// Address: use stored address, or derive from AllowedIPs mask (not iface mask).
	// Client peers have AllowedIPs=/32, so Address gets /32 — matches UI display.
	if p.Address != "" {
		fmt.Fprintf(&sb, "Address = %s\n", p.Address)
	} else if p.AllowedIPs != "" {
		firstCIDR := strings.TrimSpace(strings.Split(p.AllowedIPs, ",")[0])
		peerIP := strings.SplitN(firstCIDR, "/", 2)[0]
		mask := "32"
		if parts := strings.SplitN(firstCIDR, "/", 2); len(parts) == 2 {
			mask = parts[1]
		}
		if peerIP != "" && peerIP != "0.0.0.0" {
			fmt.Fprintf(&sb, "Address = %s/%s\n", peerIP, mask)
		}
	}

	dns := iface.DNS
	if dns == "" {
		dns = "1.1.1.1, 8.8.8.8"
	}
	fmt.Fprintf(&sb, "DNS = %s\n", dns)

	// AmneziaWG 2.0 obfuscation parameters (must match exactly on both sides).
	if iface.Protocol == "amneziawg-2.0" && iface.Settings != nil {
		s := iface.Settings
		fmt.Fprintf(&sb, "Jc = %d\n", s.Jc)
		fmt.Fprintf(&sb, "Jmin = %d\n", s.Jmin)
		fmt.Fprintf(&sb, "Jmax = %d\n", s.Jmax)
		fmt.Fprintf(&sb, "S1 = %d\n", s.S1)
		fmt.Fprintf(&sb, "S2 = %d\n", s.S2)
		fmt.Fprintf(&sb, "S3 = %d\n", s.S3)
		fmt.Fprintf(&sb, "S4 = %d\n", s.S4)
		fmt.Fprintf(&sb, "H1 = %s\n", s.H1)
		fmt.Fprintf(&sb, "H2 = %s\n", s.H2)
		fmt.Fprintf(&sb, "H3 = %s\n", s.H3)
		fmt.Fprintf(&sb, "H4 = %s\n", s.H4)
		if s.I1 != "" { fmt.Fprintf(&sb, "I1 = %s\n", s.I1) }
		if s.I2 != "" { fmt.Fprintf(&sb, "I2 = %s\n", s.I2) }
		if s.I3 != "" { fmt.Fprintf(&sb, "I3 = %s\n", s.I3) }
		if s.I4 != "" { fmt.Fprintf(&sb, "I4 = %s\n", s.I4) }
		if s.I5 != "" { fmt.Fprintf(&sb, "I5 = %s\n", s.I5) }
	}

	sb.WriteString("\n[Peer]\n")
	fmt.Fprintf(&sb, "PublicKey = %s\n", iface.PublicKey)

	if p.PresharedKey != "" {
		fmt.Fprintf(&sb, "PresharedKey = %s\n", p.PresharedKey)
	}

	if iface.Host != "" {
		fmt.Fprintf(&sb, "Endpoint = %s:%d\n", iface.Host, iface.ListenPort)
	}

	clientAllowedIPs := p.ClientAllowedIPs
	if clientAllowedIPs == "" {
		clientAllowedIPs = iface.DefaultClientAllowedIPs
	}
	if clientAllowedIPs == "" {
		clientAllowedIPs = "0.0.0.0/0, ::/0"
	}
	fmt.Fprintf(&sb, "AllowedIPs = %s\n", clientAllowedIPs)
	fmt.Fprintf(&sb, "PersistentKeepalive = %d\n", p.PersistentKeepalive)

	return sb.String()
}

// generateTemplateConfig produces an instructional config for manually-configured peers.
func (p *Peer) generateTemplateConfig(iface InterfaceData) string {
	proto := "WireGuard 1.0"
	bin := "wg-quick"
	if iface.Protocol == "amneziawg-2.0" {
		proto = "AmneziaWG 2.0"
		bin = "awg-quick"
	}

	var sb strings.Builder
	sb.WriteString("# ═══════════════════════════════════════════════════════════════\n")
	fmt.Fprintf(&sb, "# Remote Configuration for: %s\n", p.Name)
	fmt.Fprintf(&sb, "# Connect to: %s\n", iface.Name)
	fmt.Fprintf(&sb, "# Protocol: %s\n", proto)
	sb.WriteString("# ═══════════════════════════════════════════════════════════════\n\n")

	sb.WriteString("[Interface]\n")
	sb.WriteString("# IMPORTANT: Generate your own private key on remote side:\n")
	sb.WriteString("#   wg genkey > privatekey\n")
	sb.WriteString("#   cat privatekey | wg pubkey > publickey\n")
	sb.WriteString("# Then replace YOUR_PRIVATE_KEY with content of privatekey\n")
	sb.WriteString("PrivateKey = YOUR_PRIVATE_KEY\n\n")
	sb.WriteString("# Listen port (choose any free UDP port)\n")
	sb.WriteString("ListenPort = 51820\n\n")

	if p.Address != "" {
		fmt.Fprintf(&sb, "Address = %s\n", p.Address)
	} else if p.AllowedIPs != "" {
		firstCIDR := strings.TrimSpace(strings.Split(p.AllowedIPs, ",")[0])
		peerIP := strings.SplitN(firstCIDR, "/", 2)[0]
		mask := "32"
		if parts := strings.SplitN(firstCIDR, "/", 2); len(parts) == 2 {
			mask = parts[1]
		}
		if peerIP != "" && peerIP != "0.0.0.0" {
			fmt.Fprintf(&sb, "Address = %s/%s\n", peerIP, mask)
		}
	}

	dns := iface.DNS
	if dns == "" {
		dns = "1.1.1.1, 8.8.8.8"
	}
	fmt.Fprintf(&sb, "DNS = %s\n\n", dns)

	if iface.Protocol == "amneziawg-2.0" && iface.Settings != nil {
		s := iface.Settings
		sb.WriteString("# AmneziaWG 2.0 Parameters (MUST match EXACTLY on both sides!)\n")
		fmt.Fprintf(&sb, "Jc = %d\nJmin = %d\nJmax = %d\n", s.Jc, s.Jmin, s.Jmax)
		fmt.Fprintf(&sb, "S1 = %d\nS2 = %d\nS3 = %d\nS4 = %d\n", s.S1, s.S2, s.S3, s.S4)
		fmt.Fprintf(&sb, "H1 = %s\nH2 = %s\nH3 = %s\nH4 = %s\n", s.H1, s.H2, s.H3, s.H4)
		if s.I1 != "" { fmt.Fprintf(&sb, "I1 = %s\n", s.I1) }
		if s.I2 != "" { fmt.Fprintf(&sb, "I2 = %s\n", s.I2) }
		if s.I3 != "" { fmt.Fprintf(&sb, "I3 = %s\n", s.I3) }
		if s.I4 != "" { fmt.Fprintf(&sb, "I4 = %s\n", s.I4) }
		if s.I5 != "" { fmt.Fprintf(&sb, "I5 = %s\n", s.I5) }
		sb.WriteString("\n")
	}

	sb.WriteString("[Peer]\n")
	fmt.Fprintf(&sb, "PublicKey = %s\n\n", iface.PublicKey)

	host := iface.Host
	if host == "" {
		host = "YOUR_HUB_PUBLIC_IP"
	}
	fmt.Fprintf(&sb, "Endpoint = %s:%d\n", host, iface.ListenPort)

	clientAllowedIPs := p.ClientAllowedIPs
	if clientAllowedIPs == "" {
		clientAllowedIPs = iface.DefaultClientAllowedIPs
	}
	if clientAllowedIPs == "" {
		clientAllowedIPs = "0.0.0.0/0, ::/0"
	}
	fmt.Fprintf(&sb, "AllowedIPs = %s\n", clientAllowedIPs)
	fmt.Fprintf(&sb, "PersistentKeepalive = %d\n\n", p.PersistentKeepalive)

	sb.WriteString("# ═══════════════════════════════════════════════════════════════\n")
	sb.WriteString("# 1. Generate keys: wg genkey | tee privatekey | wg pubkey > publickey\n")
	sb.WriteString("# 2. Replace YOUR_PRIVATE_KEY above\n")
	sb.WriteString("# 3. Add your public key to hub peer configuration\n")
	fmt.Fprintf(&sb, "# 4. Run: %s up wg0\n", bin)
	sb.WriteString("# ═══════════════════════════════════════════════════════════════\n")

	return sb.String()
}

// ── QR code ───────────────────────────────────────────────────────────────────

// GenerateQRSVG encodes content as a QR code and returns an SVG string.
// Uses rsc.io/qr (pure Go) for encoding; renders cells as <rect> elements.
// The SVG uses a fixed cell size of 5px with 4-cell quiet zone padding.
func GenerateQRSVG(content string) (string, error) {
	code, err := qr.Encode(content, qr.Q)
	if err != nil {
		return "", fmt.Errorf("qr encode: %w", err)
	}

	const cellSize = 5
	const padding = 4 // cells (QR spec requires ≥4-module quiet zone)

	size := code.Size
	total := (size + padding*2) * cellSize

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">`,
		total, total, total, total)
	// White background (quiet zone).
	fmt.Fprintf(&sb, `<rect width="%d" height="%d" fill="white"/>`, total, total)

	// Render each black module as a filled rectangle.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if code.Black(x, y) {
				px := (x + padding) * cellSize
				py := (y + padding) * cellSize
				fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" fill="black"/>`,
					px, py, cellSize, cellSize)
			}
		}
	}

	sb.WriteString(`</svg>`)
	return sb.String(), nil
}

// ── Private: DB helpers ───────────────────────────────────────────────────────

type peerScanner interface {
	Scan(dest ...any) error
}

func scanPeer(rows *sql.Rows) (Peer, error) {
	p, err := scanPeerRow(rows)
	if err != nil {
		return Peer{}, err
	}
	return *p, nil
}

func scanPeerRow(s peerScanner) (*Peer, error) {
	var p Peer
	var enabled int

	err := s.Scan(
		&p.ID, &p.InterfaceID, &p.Name, &p.PublicKey, &p.PrivateKey, &p.PresharedKey,
		&p.Endpoint, &p.AllowedIPs, &p.Address, &p.ClientAllowedIPs,
		&p.PeerType, &p.PersistentKeepalive, &enabled,
		&p.CreatedAt, &p.UpdatedAt, &p.ExpiredAt, &p.OneTimeLink,
		&p.TotalRx, &p.TotalTx,
	)
	if err != nil {
		return nil, err
	}

	p.Enabled = enabled != 0
	p.DownloadableConfig = p.PrivateKey != ""

	if p.PeerType == "" {
		p.PeerType = "client"
	}
	if p.PersistentKeepalive == 0 {
		p.PersistentKeepalive = 25
	}
	if p.ClientAllowedIPs == "" {
		p.ClientAllowedIPs = "0.0.0.0/0, ::/0"
	}

	return &p, nil
}

func insertPeer(p Peer) error {
	_, err := db.DB().Exec(`
		INSERT INTO peers
		    (id, interface_id, name, public_key, private_key, preshared_key,
		     endpoint, allowed_ips, address, client_allowed_ips,
		     peer_type, persistent_keepalive, enabled,
		     created_at, updated_at, expired_at, one_time_link)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		p.ID, p.InterfaceID, p.Name, p.PublicKey, p.PrivateKey, p.PresharedKey,
		p.Endpoint, p.AllowedIPs, p.Address, p.ClientAllowedIPs,
		p.PeerType, p.PersistentKeepalive, boolInt(p.Enabled),
		p.CreatedAt, p.UpdatedAt, p.ExpiredAt, p.OneTimeLink,
	)
	return err
}

func updatePeer(p Peer) error {
	_, err := db.DB().Exec(`
		UPDATE peers
		SET name = ?, endpoint = ?, allowed_ips = ?, address = ?,
		    client_allowed_ips = ?, persistent_keepalive = ?,
		    enabled = ?, updated_at = ?, expired_at = ?, one_time_link = ?
		WHERE id = ?
	`,
		p.Name, p.Endpoint, p.AllowedIPs, p.Address,
		p.ClientAllowedIPs, p.PersistentKeepalive,
		boolInt(p.Enabled), p.UpdatedAt, p.ExpiredAt, p.OneTimeLink,
		p.ID,
	)
	return err
}

// ── Validation + helpers ──────────────────────────────────────────────────────

func validatePeerInput(inp PeerInput) error {
	if strings.TrimSpace(inp.Name) == "" {
		return fmt.Errorf("peer name is required")
	}
	if strings.TrimSpace(inp.PublicKey) == "" {
		return fmt.Errorf("public key is required")
	}
	if len(strings.TrimSpace(inp.PublicKey)) != 44 {
		return fmt.Errorf("public key must be 44 characters (base64)")
	}
	if strings.TrimSpace(inp.AllowedIPs) == "" {
		return fmt.Errorf("allowedIPs is required")
	}
	if inp.Endpoint != "" && !isValidEndpoint(inp.Endpoint) {
		return fmt.Errorf("endpoint must be in host:port format")
	}
	return nil
}

// isValidEndpoint checks "host:port" format.
func isValidEndpoint(ep string) bool {
	idx := strings.LastIndex(ep, ":")
	if idx < 1 {
		return false
	}
	port := ep[idx+1:]
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(port) > 0
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func strOr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func intOr(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
