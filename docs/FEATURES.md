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
- Create / edit / delete persistent routes (`destination`, `via`, `dev`, `metric`, `table`)
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
- Redirect inbound traffic on any port to another server (`iptables-nft PREROUTING DNAT`)
- Protocol: `tcp`, `udp`, or `both` (expands to separate rules per protocol)
- Optional **inbound interface** scoping (`-i eth0`) — prevents intercepting WireGuard or loopback traffic
- Optional **source NAT / Masquerade** (default on) — rewrites source IP so the destination replies back through this server; required for public internet destinations
- `destPort = 0` sentinel — forward to the same port as `inPort`
- Idempotent `-C` check prevents duplicate rules on container restart
- Restored automatically on startup via `RestoreAllDnat()`
- ⚠️ Warning in UI when `inPort` is outside the configured `portPool` (bridge network mode awareness)

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

### Default Policy
- Configurable terminal action for `FIREWALL_FORWARD`: **accept** (default) or **drop**
- `accept` — passes unmatched forwarded traffic (permissive, original behaviour)
- `drop` — silently drops unmatched forwarded traffic; a `DROP all` rule is appended to the end of `FIREWALL_FORWARD` after all user rules
- Stored in SQLite (`settings` key/value table); survives container restart
- UI card on the Firewall Rules page with a select and a confirmation dialog before switching to `drop`
- Any change triggers `RebuildChains()` — the terminal rule is re-applied atomically

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
- `subnetPool` — CIDR pool for auto-assigning subnets on quick-create (e.g. `192.168.0.0/16`)
- `portPool` — port range(s) for quick-create and bridge mode (e.g. `51831-65535`; ranges and comma-lists)
- `routerName` — human-readable name shown in the sidebar
- `publicIPMode` / `publicIPManual` — control the public IP used in peer endpoint configs
- `chartType` — traffic graph style: off / line / area / bar
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
- **Go 1.23** + **Fiber v2** — single static binary, no Node.js, no npm
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
- `docker compose -f docker-compose.yml up -d`
- `BIND_ADDR=127.0.0.1` for reverse proxy deployments
- Data directory mounted at `/etc/wireguard/data` — survives container recreate
- **Network modes:** `host` (default, shares host netns) or `bridge` (Docker-published port range via `docker-compose.bridge.yml`)
- Sidebar badge shows current network mode: HOST (gray) / BRIDGE (amber) / NONE (red)
- Sidebar badge shows WireGuard implementation: KERNEL (green) / USERSPACE (blue)

---

---

## Multi-Server Management 🆕

Manage multiple Cascade routers from a single browser session.

- **Server switcher** in the sidebar — switch between local and any registered remote server
- All API calls transparently proxied through the local server; the browser never communicates with the remote directly, and the remote token stays on the backend
- **Add Remote Server** modal:
  - Login mode: username + password → token obtained automatically
  - Explicit-token mode: supply a pre-created API token directly
  - **2FA support** — if the remote has TOTP enabled, a second prompt asks for the 6-digit code
  - **Self-signed certificate support** — "Skip TLS verification" checkbox for servers with self-signed certs
- Auto-switch back to local when the remote becomes unreachable (401 / 5xx)
- Dashboard reloads automatically on server switch
- **Remotes page** — list, add, test connectivity, delete registered servers

---

## Speed Test 🆕

iperf3-based throughput measurement between any two managed Cascade servers.

- **Route modes:**
  - `Auto` — detects S2S tunnel automatically; falls back to internet
  - `Tunnel` — forces traffic through the S2S WireGuard tunnel; requires a shared subnet
  - `Internet` — direct connection via public IPs (ignores tunnel)
  - `Manual` — pick specific interfaces on both servers for bind address
- **S2S autodetect** — finds the common subnet between two servers' WireGuard interfaces and resolves the tunnel IP automatically
- **Ping check** before test — warns if the target host is unreachable via ICMP (soft warning for internet mode, hard block for tunnel)
- Async execution — `POST /run` returns `jobId` immediately; UI polls `GET /result/:jobId` every 2 s
- Results persisted to `speedtest_results` table in SQLite — full history available
- Orchestration always runs on the **local server** — remote IDs are resolved from the local DB, avoiding cross-server ID mismatch
- Requires `iperf3` installed on both source and destination servers

---

## Monitoring & Diagnostics 🆕

Real-time and historical metrics for interfaces and gateways.

### Traffic Metrics
- Per-interface RX/TX bytes/s collected every 5 seconds from `/proc/net/dev`
- Stored in a separate **`metrics.db`** (not cascade.db) to avoid WAL contention during high-frequency writes
- Configurable retention periods: 5 min (realtime), 1 h, 24 h, 7 days
- ApexCharts area graphs per interface — color-coded, animated in realtime, paused when tab is hidden
- Metrics DB optionally included in backup archive

### Gateway Status History
- Gateway up/down/degraded state sampled every monitoring tick
- Stacked bar chart showing time distribution: online / degraded / offline / admin_down
- Tooltip shows percentage breakdown per bar
- Period selector: 5 min / 1 h / 24 h / 7 days

### Diagnostics Page
- Dedicated full-screen page for monitoring widgets
- Per-interface traffic charts + gateway status history in one view
- Accessible from the sidebar

---

## Rate Limits on Client Groups 🆕

Per-IP bandwidth enforcement for groups of VPN clients using Linux Traffic Control (tc HTB).

- Configured per **client-group alias** — applies to all peers in the group
- Separate **download** and **upload** limits in kbps
- Enforced via `tc qdisc` / `tc class` / `tc filter` on the WireGuard interface — one HTB class per client IP
- Rules restored automatically on interface start/restart
- When a peer moves between groups (or expires), old tc rules are removed and new ones applied
- `0` = unlimited (no tc rule created)

---

## Planned / Not Yet Implemented

| Feature | Status |
|---------|--------|
| Admin tunnel full migration | Partial — client list returns `[]` |
| RBAC (roles: admin / operator / viewer) | Designed, not implemented |
| Telegram bot notifications | Wishlist |
| VPN-only management access | Wishlist |
| UI config via API (no docker-compose edit) | Wishlist |
