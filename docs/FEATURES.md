# Cascade — Feature Overview

Cascade is a self-hosted WireGuard / AmneziaWG router management platform.
It replaces the original AWG-Easy with a full-stack rewrite in Go + Fiber, providing
enterprise-grade routing, firewall, and monitoring capabilities through a clean web UI.

---

## Tunnel Interfaces

Multiple independent WireGuard or AmneziaWG interfaces, each with its own subnet,
port, and peer list.

- Create / edit / delete interfaces (`wg10`, `wg11`, …)
- Start / stop / restart via UI or API
- Two protocols: **WireGuard 1.0** (`wg-quick`) and **AmneziaWG 2.0** (`awg-quick`)
- Hot-reload of parameters without dropping active connections (`awg syncconf`)
- Auto-start on container restart — all `enabled=true` interfaces come up automatically
- Per-interface AWG2 obfuscation parameters (Jc, Jmin, Jmax, S1–S4, H1–H4, I1–I5)
- Export interface parameters for S2S workflow
- Backup / restore interface + all peers as JSON

---

## Peers

Two peer types per interface:

### Client peers
- Auto-generated key pair (server holds private key for QR/download)
- Client config download (`.conf` file) and QR code
- Per-peer `AllowedIPs` for split-tunnel or full-tunnel
- Enable / disable without deleting
- Optional expiry date
- One-time config link

### Interconnect (S2S) peers
- Export → Import JSON workflow for setting up site-to-site tunnels
- PSK auto-generated on import, included in re-export for the other side
- `AllowedIPs = <remote_ip>/32` for precise crypto-routing in multi-peer meshes
- Visible S2S badge + runtime endpoint (IP:port from `wg dump`, updated every second)

---

## AmneziaWG 2.0 Obfuscation

- Per-interface obfuscation parameters stored in DB
- **7 CPS profiles** for traffic imitation: QUIC Initial, QUIC 0-RTT, TLS 1.3,
  DTLS 1.3, HTTP/3, SIP, Noise_IK
- Intensity levels: `low`, `medium`, `high`
- **AWG2 Templates** — save, load, share obfuscation profiles
- **Generate (⚡)** — one-click parameter generation using the AmneziaWG-Architect
  algorithm, with optional save as template
- Non-overlapping H1–H4 ranges (4 zones of uint32 space, no collision risk)

---

## Routing

### Kernel Route Status
- View live kernel routing table (any table: `main`, `100`, `vpn_kz`, …)
- Routing tables auto-discovered from `ip rule show` — no manual configuration
- Text-based parsing (`ip route show`), never `ip -j` — works on all kernel versions

### Policy-Aware Route Lookup
- Test any destination IP with optional source IP
- Simulates PBR rules from FirewallManager (fwmark → table)
- Shows which firewall rule matched and which routing table was used
- Supports ipset membership test for alias-based rules

### Static Routes
- Create persistent routes (`destination`, `via`, `dev`, `metric`, `table`)
- Toggle enable / disable per route
- Survive container restart — restored after `tunnel.Init()` ensures interfaces exist

---

## NAT (Network Address Translation)

### Outbound Source NAT
- MASQUERADE (dynamic) and SNAT (fixed source IP) rules
- Alias support in source field (host / network / ipset / group)
- Idempotent `iptables -C` check prevents duplicate rules on restart
- Auto-rules from tunnel interfaces shown as read-only `auto` badges

### Port Forwarding (DNAT)
- Planned — placeholder in UI

---

## Gateways

Multi-gateway monitoring and failover.

### Gateway monitoring
- ICMP ping with configurable interval and sliding window (packet loss %, latency ms)
- HTTP/S probe — native Go `http.Get`, no curl subprocess
- Health decision rules: `icmp_only`, `http_only`, `both_required`, `either`
- Live status in UI: online / degraded / offline + latency + loss + HTTP code

### Gateway Groups
- Tier-based priority (tier 1 = primary, tier 2 = backup, …)
- Trigger types: `packetloss`, `latency`, `packetloss_latency`
- Group-level failover: fallback activates only when ALL members of a tier are down

### Automatic Failover
- `fallbackToDefault` flag per firewall rule
- When gateway goes down: inject `blackhole default` or `default via <system-gw>` into PBR table
- 30-second anti-flap delay before restoring original gateway route
- Uses `ip route replace` (idempotent) — never fails on stale routes

---

## Firewall

Unified packet filter + Policy-Based Routing in one rule list.

### Filter rules
- ACCEPT / DROP / REJECT actions
- Match by: interface, protocol (any/tcp/udp/icmp), source, destination
- Source / destination: any, CIDR, or alias (host / network / ipset / group)
- L4 port matching via port / port-group aliases
- Inverted match (`not source`, `not destination`)
- Enable / disable per rule
- Reorder with ↑ / ↓ buttons

### Policy-Based Routing (PBR)
- Assign a gateway (or gateway group) to any rule → traffic is marked and routed through that gateway
- Automatic: `iptables MARK` + `ip route table N` + `ip rule fwmark N lookup N`
- `fallbackToDefault` — graceful degradation when gateway is unreachable

### Implementation
- Custom iptables chains: `FIREWALL_FORWARD` (filter) and `FIREWALL_MANGLE` (mangle/PREROUTING)
- `FIREWALL_FORWARD` inserted at position 1 in the FORWARD chain (before all interface rules)
- `_rebuildChains()` — atomic flush + re-apply on any change

---

## Firewall Aliases

Reusable named objects for use in firewall rules and NAT.

| Type | Description |
|------|-------------|
| `host` | Single IP addresses |
| `network` | CIDR ranges |
| `ipset` | Large prefix sets stored in kernel ipset (millions of entries) |
| `group` | Combines multiple host / network aliases |
| `port` | L4 port entries (`tcp:443`, `udp:53`, `any:80`, `tcp:8080-8090`) |
| `port-group` | Combines multiple port aliases |

### ipset generation
- Manual upload (one CIDR per line)
- Auto-generate from **RIPE NCC** and **ipdeny** by country or ASN
- Async job with status polling
- CIDR aggregation (collapse_addresses) before loading into kernel
- Snapshots saved to disk (`*.save`) — restored automatically on container restart

---

## Security

### Authentication
- **Multi-user accounts** — each user has own username, password, TOTP
- **TOTP (2FA)** — TOTP setup via QR code (Google Authenticator, Authy, etc.)
  - Two-step login: password → 6-digit code
  - Enable / disable per user, requires current TOTP code to disable
- **API Tokens** — long-lived bearer tokens for programmatic access
  - Format: `ws_` + 64 hex chars (256 bits entropy)
  - Only SHA-256 hash stored in DB — raw value shown once
  - `last_used` timestamp updated on every authenticated request
  - Bypass TOTP — designed for scripts and automation
  - Revoke instantly from UI

### Network security
- `BIND_ADDR=127.0.0.1` — Cascade binds to localhost only, not exposed directly
- **Caddy reverse proxy** with hidden `ADMIN_PATH` prefix
  - Requests to `/<secret>/...` → Cascade
  - Everything else → decoy site (StreamVault)
- Rate limiting: 5 POST requests/minute per IP (caddy-ratelimit plugin)
- TLS via **acme.sh** — Let's Encrypt certificates for bare IP addresses
  - `shortlived` profile: 6-day validity, auto-renewal every 3 days
- `Referrer-Policy: no-referrer` — admin path does not leak to external sites
- Caddy container: read-only filesystem, `cap_drop ALL`, `cap_add NET_BIND_SERVICE` only

### Open mode
- If no users exist in DB, all requests pass through (first-run convenience)
- As soon as one user is created, authentication is enforced immediately

---

## Administration (Admin Tunnel)

Legacy wg0 interface for traditional admin VPN clients.

- Manage classic WireGuard clients (mobile, laptop)
- QR code generation
- Enable / disable / delete clients

> **Note:** Admin tunnel backend is partially migrated. Client list endpoint returns empty array in the Go rewrite; full migration is planned.

---

## Settings

- Global settings: DNS, default keepalive, default client AllowedIPs
- Gateway monitoring thresholds (global defaults)
- AWG2 Templates: CRUD + set default
- AWG2 parameter generator (⚡): 7 CPS profiles, 3 intensity levels, optional save

---

## API

Full REST API — every UI action is available programmatically.

- Session-based auth (cookie) for Web UI
- Bearer token auth (`Authorization: Bearer ws_...`) for scripts
- All list endpoints return named wrappers (never bare arrays)
- Toggle via `PATCH { enabled: bool }` — minimal payload
- Errors return `{ error: "message" }` with appropriate HTTP status

See [API.en.md](API.en.md) for the full endpoint reference.

---

## Infrastructure

### Runtime
- **Go 1.22** + **Fiber v2** — single static binary, no Node.js, no npm
- **SQLite** (modernc.org/sqlite, pure Go, no CGO) — single `wireguard.db` file
- WAL journal mode — concurrent reads, serialised writes
- Version-based migrations — schema evolves safely across upgrades
- `--network host` — WireGuard UDP ports are immediately accessible without port mapping

### Container
- Base image: Alpine Linux
- AmneziaWG + WireGuard kernel modules (DKMS, host kernel)
- iproute2, iptables-nft, ipset included
- Graceful shutdown on SIGTERM / SIGINT

### Deployment
- `docker compose -f docker-compose.go.yml up -d`
- `BIND_ADDR=127.0.0.1` for reverse proxy deployments
- Data directory mounted at `/etc/wireguard/data` — survives container recreate

---

## Planned / Not Yet Implemented

| Feature | Status |
|---------|--------|
| Port Forwarding (DNAT) | Backend + UI — not started |
| Admin tunnel full migration | Partial — client list returns `[]` |
| RBAC (roles: admin / operator / viewer) | Designed, not implemented |
| Telegram bot notifications | Wishlist |
| VPN-only management access | Wishlist |
| UI config via API (no docker-compose edit) | Wishlist |
