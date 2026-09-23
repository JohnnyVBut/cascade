# Cascade — API Reference (Go Rewrite)

> **Base URL:** `/api`
> **Auth:** All routes except session, lang, release, remember-me and UI-flag stubs require either a valid session cookie **or** an API token (`Authorization: Bearer ws_...`).
> **Content-Type:** `application/json`

---

## Authentication

### Session (Web UI)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/session` | Current session state. Returns `{ authenticated, requiresPassword, totp_pending, username }` |
| `POST` | `/api/session` | Login step 1. Body: `{ username, password, remember? }`. Returns `{ authenticated: true }` or `{ totp_required: true }` |
| `DELETE` | `/api/session` | Logout |
| `POST` | `/api/auth/totp/verify` | Login step 2 (TOTP). Body: `{ code }`. Returns `{ authenticated: true }`. Requires `totp_pending` session. |

### Users management

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/users` | List all users. Returns `{ users: [...] }` |
| `POST` | `/api/users` | Create user. Body: `{ username, password }`. Returns `{ user }` |
| `GET` | `/api/users/me` | Current user info |
| `PATCH` | `/api/users/me` | Change own password. Body: `{ password }` |
| `PATCH` | `/api/users/:id` | Update username or password. Body: `{ username?, password? }` |
| `DELETE` | `/api/users/:id` | Delete user (cannot delete the last user) |
| `POST` | `/api/users/:id/set-admin` | Grant or revoke admin role. Body: `{ admin: bool }`. Admin only. Cannot revoke the last admin |

### TOTP (2FA) setup

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/users/me/totp/setup` | Generate TOTP secret. Returns `{ secret, qr_uri, qr_png }`. Secret stored in session until confirmed. |
| `POST` | `/api/users/me/totp/enable` | Confirm and activate TOTP. Body: `{ code }` |
| `POST` | `/api/users/me/totp/disable` | Deactivate TOTP. Body: `{ code }` (current TOTP code required) |

### API Tokens (programmatic access)

Long-lived tokens for scripts and automation. No TOTP required.
Token format: `ws_` + 64 hex chars. Only SHA-256 hash is stored — raw value shown once at creation.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/tokens` | List current user's tokens. Returns `{ tokens: [{id, name, last_used, created_at}] }` |
| `POST` | `/api/tokens` | Create token. Body: `{ name }`. Returns `{ token, raw_token }` — `raw_token` shown **once** |
| `DELETE` | `/api/tokens/:id` | Revoke token |

**Usage:**
```bash
# Login to get session cookie
curl -c /tmp/ws.cookie -X POST https://<IP>/<ADMIN_PATH>/api/session \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"..."}'

# Use Bearer token (no session, no TOTP)
curl -H "Authorization: Bearer ws_<token>" \
  https://<IP>/<ADMIN_PATH>/api/tunnel-interfaces
```

---

## Version & Updates

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/version` | ❌ public | Current version + latest release info from GitHub. Response: `{ version, gitCommit, latestVersion, releaseURL, updateAvailable: bool, checkedAt, error? }` |
| `POST` | `/api/version/check` | ❌ public | Force an immediate GitHub release check, bypassing the 24 h cache. Returns the same shape as `GET /api/version`. |
| `GET` | `/api/health` | ❌ public | Health check. Response: `{ status: "ok", version, host }` |

`version` is `"dev"` for local builds without ldflags. Injected at build time via:
```
-ldflags "-X ...version.Version=v1.2.3 -X ...version.GitCommit=abc1234"
```
Update check polls `https://api.github.com/repos/JohnnyVBut/cascade/releases/latest` every 24 h.
First check happens 10 s after startup. Results are cached in memory — `/api/version` always returns instantly.

---

## Settings

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/settings` | Global settings + runtime info |
| `PUT` | `/api/settings` | Partial update. Body: see below |

**GET /api/settings — response fields:**

Returns `GlobalSettings` merged with runtime-only fields:

| Field | Type | Description |
|-------|------|-------------|
| `dns` | string | DNS server for client configs |
| `mtu` | int | MTU for client configs. `0` = not set (WireGuard picks automatically). Range: 576–9000 |
| `defaultPersistentKeepalive` | int | Default keepalive (seconds) |
| `defaultClientAllowedIPs` | string | Default AllowedIPs for new client peers |
| `gatewayWindowSeconds` | int | Gateway monitoring sliding window (seconds) |
| `gatewayHealthyThreshold` | int | Healthy threshold (% packet loss) |
| `gatewayDegradedThreshold` | int | Degraded threshold (% packet loss) |
| `subnetPool` | string | CIDR pool for auto-assigning subnets on quick-create, e.g. `"192.168.0.0/16"`. Must be a network address. Invalid value → **400** |
| `portPool` | string | Port pool for quick-create, e.g. `"51831-65535"` (ranges and comma-lists supported). Invalid value → **400** |
| `defaultFwPolicy` | string | Default firewall policy: `"accept"` or `"drop"`. Default `"accept"` |
| `routerName` | string | Human-readable router name (shown in sidebar) |
| `publicIPMode` | string | Public IP resolution mode: `"auto"` or `"manual"` |
| `publicIPManual` | string | Manual public IP (used when `publicIPMode="manual"`) |
| `chartType` | int | Traffic chart type: `0`=off, `1`=line, `2`=area, `3`=bar |
| `hostname` | string | *(runtime)* Container hostname |
| `resolvedPublicIP` | string | *(runtime)* Resolved public IP for peer endpoints |
| `publicIPWarning` | string | *(runtime)* Warning if public IP is unavailable |
| `awgMode` | string | *(runtime)* `"kernel"` or `"userspace"` (amneziawg-go) |
| `networkMode` | string | *(runtime)* `"host"`, `"bridge"`, or `"none"` — Docker network mode |

**PUT /api/settings — accepted fields:**

`{ dns?, mtu?, defaultPersistentKeepalive?, defaultClientAllowedIPs?, gatewayWindowSeconds?, gatewayHealthyThreshold?, gatewayDegradedThreshold?, subnetPool?, portPool?, defaultFwPolicy?, routerName?, publicIPMode?, publicIPManual?, chartType?, lang? }`

`lang` — UI language: `"en"` or `"ru"`. Also reflected in `GET /api/lang`.

`mtu` — global MTU written into client config `[Interface]` sections. Can be overridden per-interface via `PATCH /api/tunnel-interfaces/:id` (`mtu` field).

---

## AWG2 Templates

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/templates` | List all templates |
| `POST` | `/api/templates` | Create template. Body: `{ name, jc, jmin, jmax, s1–s4, h1–h4, i1–i5 }` |
| `GET` | `/api/templates/:id` | Get template |
| `PUT` | `/api/templates/:id` | Update template |
| `DELETE` | `/api/templates/:id` | Delete template |
| `POST` | `/api/templates/:id/set-default` | Set as default |
| `POST` | `/api/templates/:id/apply` | Apply — returns AWG2 params with fresh H1-H4 |
| `POST` | `/api/templates/generate` | Generate AWG2 params. Body: `{ profile, intensity, host?, browser?, saveName? }`. profile: random|quic_initial|quic_0rtt|tls_client_hello|dtls|http3|sip|wireguard_noise|**dns_query**|tls_to_quic|quic_burst. browser: chrome|firefox|safari|edge|yandex_desktop|yandex_mobile (not applicable for sip and dns_query) |

---

## Tunnel Interfaces

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/tunnel-interfaces` | List interfaces. Returns `{ interfaces: [...] }` |
| `POST` | `/api/tunnel-interfaces` | Create. Body: `{ name, address, listenPort, protocol, disableRoutes?, natDisabled?, settings? }` |
| `POST` | `/api/tunnel-interfaces/quick-create` | Quick-create: create and start a client interface in one step. Body: `{ name?: string, protocol?: string }`. Address and port are auto-assigned from SubnetPool/PortPool settings. AWG2 params come from the default template or a random profile. Response: `{ interface, started: bool, startError?: string }` |
| `POST` | `/api/tunnel-interfaces/import-conf` | Import a WireGuard/AmneziaWG client `.conf` file as an uplink (client-mode) interface. `DisableRoutes` is always set to `true` — the kernel routing table is not modified. Body: `{ name: string, conf: string }`. Response: `{ interface, peer, started: bool, startError?: string, conflictWarning?: string }` |
| `POST` | `/api/tunnel-interfaces/import-backup` | Import an AWG-Easy JSON backup. Creates a new interface with all clients from the file. Server and client keys are preserved as-is — existing client configs remain valid without reissue. Body: `{ json: string, listenPort: int }`. Response: `{ interface, peersCreated: int, peersFailed?: string[], started: bool, startError?: string }`. Port or subnet conflict → **400** |
| `GET` | `/api/tunnel-interfaces/:id` | Get interface |
| `PATCH` | `/api/tunnel-interfaces/:id` | Update (hot-reload via syncconf). Body: `{ name?, address?, listenPort?, natDisabled?, publicHost?, mtu?, settings? }`. `publicHost` overrides the global Public IP for this interface's peer configs (useful for transit/relay setups). `mtu` overrides the global MTU for this interface (`0` = use global). Changing `natDisabled` on a running interface triggers `Restart()` |
| `DELETE` | `/api/tunnel-interfaces/:id` | Delete interface |
| `POST` | `/api/tunnel-interfaces/:id/start` | Start. Returns `{ interface }` |
| `POST` | `/api/tunnel-interfaces/:id/stop` | Stop. Returns `{ interface }` |
| `POST` | `/api/tunnel-interfaces/:id/restart` | Restart. Returns `{ interface }` |
| `GET` | `/api/tunnel-interfaces/:id/export-params` | S2S export. Returns `{ name, publicKey, endpoint, address, protocol, presharedKey? }` |
| `GET` | `/api/tunnel-interfaces/:id/export-obfuscation` | AWG2 obfuscation params as JSON |
| `GET` | `/api/tunnel-interfaces/:id/export` | Export full interface (including private key) + optionally all peers as JSON, for cloning/migrating an interface to another server. Query: `?peers=0` to omit peers (default: included) |
| `POST` | `/api/tunnel-interfaces/import-interface` | Import an interface previously produced by `GET /:id/export`. Body: `{ json: string, listenPort: int }` |
| `GET` | `/api/tunnel-interfaces/:id/backup` | Download interface + all peers as JSON |
| `PUT` | `/api/tunnel-interfaces/:id/restore` | Restore peers from backup. Removes existing peers first |

---

## Peers

Base path: `/api/tunnel-interfaces/:id/peers`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/peers` | List peers. Returns `{ peers: [...] }` |
| `POST` | `/peers` | Create peer. Body: `{ name, peerType (client/interconnect), clientAllowedIPs?, persistentKeepalive?, expiredAt? }`. Response includes `totalRx`/`totalTx` (lifetime traffic counters from SQLite, persist across restarts) and `latestHandshakeAt` (last handshake timestamp, persisted across restarts; `null` if peer never connected) |
| `POST` | `/peers/import-json` | Create interconnect peer from exported JSON |
| `POST` | `/peers/import-client-configs` | Match uploaded WireGuard client `.conf` files against existing peers by public key (derived from each file's private key) and save the private key, unlocking QR code / config download for peers that were created without one (e.g. imported from an AWG-Easy backup). Multipart field `configs` (multiple files) |
| `GET` | `/peers/:peerId` | Get peer |
| `PATCH` | `/peers/:peerId` | Update peer fields. Accepts: `name?, endpoint?, allowedIPs?, clientAllowedIPs?, persistentKeepalive?, enabled?, expiredAt?, oneTimeLink?, rateDown?, rateUp?`. Fields `rateDown`/`rateUp` — bandwidth limit in **kbps** (0 = unlimited), enforced via `tc HTB + police` on the server; the UI accepts **Mbit/s** and converts automatically |
| `DELETE` | `/peers/:peerId` | Delete peer |
| `GET` | `/peers/:peerId/config` | Download WireGuard config file |
| `GET` | `/peers/:peerId/qrcode.svg` | QR code SVG (client peers only) |
| `POST` | `/peers/:peerId/enable` | Enable peer |
| `POST` | `/peers/:peerId/disable` | Disable peer |
| `PUT` | `/peers/:peerId/name` | Rename peer. Body: `{ name }` |
| `PUT` | `/peers/:peerId/address` | Update overlay address. Body: `{ address }` → stored as AllowedIPs |
| `PUT` | `/peers/:peerId/expireDate` | Set expiry. Body: `{ expireDate }` — RFC3339 or YYYY-MM-DD, empty clears |
| `POST` | `/peers/:peerId/generateOneTimeLink` | Generate one-time config link token. Returns `{ oneTimeLink: "https://..." }`. Token is single-use — cleared after first download. |
| `GET` | `/peers/:peerId/export-json` | Export interconnect peer as JSON (interconnect only) |

### One-time config download (public)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/cnf/:token` | ❌ public | Download WireGuard config by one-time token (32 hex chars). Returns the `.conf` file as `text/plain` attachment. Token is invalidated immediately after download. Returns **404** if token is invalid or already used. |

> The `/cnf/*` path is proxied by Caddy **outside** the admin path — accessible without knowing the hidden admin URL.

---

## Routing

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/routing/table` | Kernel routes. Query: `?table=main` (default) |
| `GET` | `/api/routing/tables` | Routing tables from `ip rule show`. Returns `{ tables: [...] }` |
| `GET` | `/api/routing/test` | Route lookup. Query: `?ip=<dst>[&src=<src>][&mark=<fwmark>]`. With `src`: SimulateTrace (PBR) → `ip route get <dst> mark <fwmark>`. Returns `{ result, matchedRule, steps }` |
| `GET` | `/api/routing/routes` | Static routes (DB). Returns `{ routes: [...] }` |
| `POST` | `/api/routing/routes` | Create static route. Body: see below |
| `PATCH` | `/api/routing/routes/:id` | Update or toggle: `{ enabled: bool }` |
| `DELETE` | `/api/routing/routes/:id` | Delete route |

**Route structure (POST/PATCH body):**

| Field | Type | Description |
|-------|------|-------------|
| `destination` | string | CIDR or `"default"` (required) |
| `gateway` | string | Manual next-hop IP. Manual mode only |
| `dev` | string | Interface name (optional in manual mode) |
| `gatewayId` | string | Gateway ID from Gateways section — `via`/`dev` resolved automatically |
| `gatewayGroupId` | string | Gateway Group ID — **automatic failover** between tiers when gateway goes down |
| `metric` | int | Route metric (optional) |
| `table` | string | Routing table (default `"main"`) |
| `description` | string | Description (optional) |

> `gateway`/`dev` and `gatewayId`/`gatewayGroupId` are mutually exclusive — set one of the three.
> `gatewayId` and `gatewayGroupId` are mutually exclusive.

**Failover with GatewayGroup:**
When a route is bound to a gateway group (`gatewayGroupId`):
- Normal operation: route goes via tier 1 gateway (highest priority)
- When tier 1 goes down (status `"down"` from GatewayMonitor): immediate switch to tier 2
- When tier 1 recovers: switch back to tier 1 after 30 s (anti-flap)

---

## NAT

### Outbound Source NAT

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/nat/interfaces` | Host network interfaces. Returns `{ interfaces: [...] }` |
| `GET` | `/api/nat/rules` | NAT rules + auto-rules from tunnel interfaces. Returns `{ rules: [...] }`. Auto-rules have `"auto": true` (read-only) |
| `POST` | `/api/nat/rules` | Create rule. Body: `{ name, source?, sourceAliasId?, outInterface, type (MASQUERADE/SNAT), toSource? (SNAT only), comment? }` |
| `PATCH` | `/api/nat/rules/:id` | Update or toggle: `{ enabled: bool }` |
| `DELETE` | `/api/nat/rules/:id` | Delete rule |

### Port Forwarding (DNAT)

Redirects inbound traffic to another host via `iptables-nft PREROUTING DNAT`.
Each rule creates up to 4 iptables commands per protocol: PREROUTING DNAT + 2× FORWARD ACCEPT + optional POSTROUTING MASQUERADE.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/nat/dnat` | List DNAT rules. Returns `{ rules: [...] }` |
| `POST` | `/api/nat/dnat` | Create rule. Body: see below |
| `PATCH` | `/api/nat/dnat/:id` | Update or toggle: `{ enabled: bool }` |
| `DELETE` | `/api/nat/dnat/:id` | Delete rule |

**DnatRule fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | ✓ | Rule name |
| `protocol` | string | ✓ | `"tcp"` / `"udp"` / `"both"` |
| `inInterface` | string | | Inbound interface (`"eth0"`, `"ens3"`, …). Empty = any |
| `inPort` | int | ✓ | Inbound port 1–65535 |
| `destIP` | string | ✓ | Destination IP (target server) |
| `destPort` | int | | Destination port 0–65535. `0` = same as `inPort` |
| `masquerade` | bool | | Add POSTROUTING MASQUERADE. **Default: `true`**. Required when the target is a public server with no route back through this machine |
| `comment` | string | | Optional comment |
| `enabled` | bool | | Status (always `true` on creation) |

> **Note on masquerade:** disable only when the target host is connected via a WireGuard
> hub-and-spoke tunnel that already routes replies back through this server.

---

## Gateways

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/gateways` | List gateways with live status. Returns `{ gateways: [...] }` |
| `POST` | `/api/gateways` | Create gateway. Body: `{ name, interface, gatewayIP, monitorAddress?, interval?, windowSeconds?, healthyThreshold?, degradedThreshold?, monitorHttp? }` |
| `GET` | `/api/gateways/:id` | Get gateway |
| `PATCH` | `/api/gateways/:id` | Update gateway |
| `DELETE` | `/api/gateways/:id` | Delete gateway |

### Gateway Groups

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/gateway-groups` | List groups. Returns `{ groups: [...] }` |
| `POST` | `/api/gateway-groups` | Create group. Body: `{ name, members: [{gatewayId, tier}], trigger (packetloss/latency/packetloss_latency) }` |
| `GET` | `/api/gateway-groups/:id` | Get group |
| `PATCH` | `/api/gateway-groups/:id` | Update group |
| `DELETE` | `/api/gateway-groups/:id` | Delete group |

---

## Firewall

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/firewall/interfaces` | Host interfaces for rule binding. Returns `{ interfaces: [...] }` |
| `GET` | `/api/firewall/rules` | Rules sorted by `order`. Returns `{ rules: [...] }` |
| `POST` | `/api/firewall/rules` | Create rule. Body: `{ name?, interface?, protocol?, source (Endpoint), destination (Endpoint), action (accept/drop/reject), gatewayId?, gatewayGroupId?, fallbackToDefault?, comment?, enabled? }` |
| `PATCH` | `/api/firewall/rules/:id` | Update or toggle: `{ enabled: bool }` |
| `DELETE` | `/api/firewall/rules/:id` | Delete rule |
| `POST` | `/api/firewall/rules/:id/move` | Reorder by one step. Body: `{ direction: "up"\|"down" }` |
| `POST` | `/api/firewall/reorder` | Reorder all rules at once. Body: `{ ids: ["id1", "id2", ...] }` — full ordered list of all rule IDs, must contain exactly the current IDs (no extras, none missing) |
| `GET` | `/api/firewall/pending` | Whether the draft rule set differs from the last applied kernel snapshot. Returns `{ hasPendingChanges: bool }` |
| `POST` | `/api/firewall/apply` | Copy draft → applied snapshot and rebuild iptables chains. **204** on success |
| `POST` | `/api/firewall/discard` | Revert draft to the last applied snapshot — no kernel change. **204** on success |

> Firewall rule changes are staged as a "draft" and only take effect in the kernel after
> `POST /apply` — check `GET /pending` and call `/apply` (or `/discard` to abandon) after any
> create/update/delete/reorder call, or edits will sit unapplied.

### Endpoint object

```json
{
  "type": "any | cidr | alias",
  "value": "10.0.0.0/8",
  "aliasId": "<uuid>",
  "portAliasId": "<uuid>",
  "invert": false
}
```

---

## Aliases

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/aliases` | List aliases. Returns `{ aliases: [...] }` |
| `POST` | `/api/aliases` | Create alias. Body: `{ name, type, entries?, comment? }` |
| `GET` | `/api/aliases/client-groups` | List aliases of type `client-group` (used by peer create/edit dropdowns). Returns `{ groups: [...] }` |
| `GET` | `/api/aliases/:id` | Get alias |
| `PATCH` | `/api/aliases/:id` | Update alias |
| `DELETE` | `/api/aliases/:id` | Delete alias |
| `POST` | `/api/aliases/:id/upload` | Upload prefix list. Body: `{ content: "..." }` |
| `POST` | `/api/aliases/:id/generate` | Generate ipset from RIPE/ipdeny. Body: `{ country?, asn?, asnList? }`. Returns `{ jobId }` |
| `GET` | `/api/aliases/:id/generate/:jobId` | Poll job status. Returns `{ status: "running"\|"done"\|"error", entryCount?, error? }` |

### Alias types

| Type | Entries format | Use |
|------|---------------|-----|
| `host` | `["1.2.3.4"]` | Single IPs |
| `network` | `["10.0.0.0/8"]` | CIDR ranges |
| `ipset` | generated | Large prefix sets (kernel ipset) |
| `group` | `["<aliasId>"]` | Combines host/network aliases |
| `client-group` | managed automatically | Kernel ipset populated with IPs of peers belonging to the group. Managed automatically on peer create/update/delete. Used in firewall rules for per-group traffic control. |
| `port` | `["tcp:443", "udp:53", "any:80"]` | L4 ports |
| `port-group` | `["<portAliasId>"]` | Combines port aliases |

---

## Dashboard

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/dashboard/widgets` | Saved widget layout for the current user. Query: `?page=dashboard` (default) or `?page=diagnostics`. Returns `{ "widgets": [...] }` — the array shape is defined by the frontend, opaque to the API |
| `PUT` | `/api/dashboard/widgets` | Save widget layout. Same `?page=` query. Body: `{ "widgets": [...] }` |
| `GET` | `/api/dashboard/system-info` | Host system metrics for the dashboard's System Info card |

**GET /api/dashboard/system-info — response fields:**

| Field | Type | Description |
|-------|------|-------------|
| `hostname` | string | |
| `uptime` | string | Human-readable, e.g. `"3d 4h 12m"` |
| `uptimeSec` | int | |
| `load1`, `load5`, `load15` | float | `/proc/loadavg` |
| `memTotal`, `memFree`, `memUsed` | int | kB. `memFree` uses `MemAvailable` when present |
| `memPct` | int | 0–100 |
| `awgCliVersion` | string | Kernel mode only. `""` if undetectable |
| `awgKernelVersion` | string | Kernel mode only. `""` if undetectable or in userspace mode |
| `awgVersionMismatch` | bool | `true` if the AWG CLI and loaded kernel module major.minor versions differ — see [Troubleshooting](../README.md#️-troubleshooting) |

> Each saved widget row referencing a since-deleted gateway (`"gateway:<id>"` in `graphs`/
> `graphColors`) is silently pruned on the next `GET /widgets` for that user/page — self-healing,
> no separate cleanup endpoint needed.

---

## Metrics

Real-time and historical system/gateway metrics, backed by an in-process sampler and SQLite history.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/metrics` | Current snapshot: CPU, RAM, per-interface network throughput, per-gateway status. Returns `{ cpu, mem, memUsedMb, memTotalMb, net: {iface: {rxMbps, txMbps}}, interfaces: [...], gateways: {id: status} }` |
| `GET` | `/api/metrics/history` | Historical points for one metric key. Query: `?key=cpu&period=5m\|1h\|6h\|24h\|7d\|30d` (default `5m`). Returns `{ key, period, points: [[timestamp, value], ...] }` |
| `GET` | `/api/metrics/gateway-dist` | Per-bucket gateway status distribution, for the Diagnostics status bar chart. Query: `?key=gateway:<id>&period=1h` (`key` must start with `gateway:`). Returns `{ key, period, buckets: [[ts_ms, healthyCount, degradedCount, downCount, adminDownCount], ...] }` |

`period` determines both the lookback window and the bucket size: `5m`→5s buckets, `1h`→60s,
`6h`→300s, `24h`→900s, `7d`→3600s, `30d`→21600s.

---

## Diagnostics

Ad-hoc network troubleshooting tools, run on the server on demand. Streaming endpoints use
Server-Sent Events (SSE) — each line of live command output arrives as one `data: <line>\n\n`
event; the stream ends with a `data: [done]\n\n` event.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/diagnostics/ping` | One-shot ping, JSON result (not streamed). Body: `{ host, count? }` (count 1–10, default 3). Returns `{ reachable, latencyMs, packetLoss }` |
| `GET` | `/api/diagnostics/ping/stream` | Streaming ping (SSE), terminal-style output line by line. Query: `?host=...&count=1-20(default 5)&source=<iface>&size=<bytes 0-65507>&df=true&tos=0-255` |
| `GET` | `/api/diagnostics/traceroute/stream` | Streaming traceroute (SSE). Query: `?host=...&type=udp(default)\|icmp\|tcp&source=<src IP>` |
| `GET` | `/api/diagnostics/tcpdump/stream` | Streaming packet capture (SSE). Query: `?iface=<name>&filter=<BPF expression>&save=true` |
| `POST` | `/api/diagnostics/tcpdump/stop` | Stop a `save=true` capture and finalize the PCAP file. Query: `?file=<captureId>` (from the stream's `[captureid:<id>]` event) |
| `GET` | `/api/diagnostics/tcpdump/download` | Download the finalized PCAP file, then delete it from the server. Query: `?file=<captureId>` |

**tcpdump save flow:** start `GET /tcpdump/stream?iface=wg10&save=true`; the *first* SSE event is
`[captureid:<hex-id>]` — capture it immediately. Call `POST /tcpdump/stop?file=<id>` to send
`SIGINT` and flush the file, then `GET /tcpdump/download?file=<id>` to fetch it (one-shot — the
file and its registry entry are removed after download, and after a 300 s hard timeout with no
stop call).

`host`/`source`/`iface` are validated against a strict alphanumeric+`.`/`-`/`_`/`:`/`[`/`]`
character set server-side — no shell metacharacters accepted.

---

## Remotes (Multi-Server)

Lets one Cascade instance manage others: the browser only ever talks to the local server, which
proxies authenticated requests to registered remotes using a stored API token. Used for the
multi-server sidebar and cross-server Speed Test.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/remotes` | List registered remotes. Returns `{ remotes: [...] }` (tokens never included in the response) |
| `POST` | `/api/remotes` | Register a remote. Two modes — see below |
| `DELETE` | `/api/remotes/:id` | Remove a remote |
| `POST` | `/api/remotes/:id/test` | Connectivity check (pings the remote with its stored token). Returns `{ ok: true }` or a **502** error |
| `ALL` | `/api/remotes/:id/proxy/*` | Forwards the request to the remote's `/api/*`, injecting its stored Bearer token. The browser never sees the remote's credentials |

**POST /api/remotes — login mode:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Display name (required) |
| `url` | string | Remote's base URL — must resolve to a public address (SSRF-guarded) (required) |
| `username`, `password` | string | Remote's admin credentials, used once to obtain a token |
| `totpCode` | string | Only needed if the remote has 2FA — see below |
| `skipTlsVerify` | bool | Skip TLS certificate verification (self-signed certs) |

If the remote has TOTP enabled and `totpCode` is omitted, the response is **422**
`{ "totp_required": true }` — retry the same call with `totpCode` filled in.

**POST /api/remotes — explicit-token mode:** set `token` instead of `username`/`password` to
register a pre-existing API token directly (validated against the remote with a ping before
being stored) — no login performed, no 2FA flow.

> `proxyRemote` strips `Authorization`/`Cookie`/`Host` from the forwarded request and drops
> `Set-Cookie` from the remote's response, so a remote's session can never leak into or clobber
> the local browser session. Redirects are followed with the same SSRF re-check applied to
> every hop.

---

## Speed Test

On-demand `iperf3` throughput test between any two Cascade servers (or the local server and an
arbitrary host). The `run`/`result*` endpoints are the ones the UI calls directly; `server`/
`client` are internal orchestration endpoints (still reachable directly if scripting your own
test) that the *source* server calls on the *destination* server via the Remotes proxy.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/speedtest/check` | Whether `iperf3` is installed on this server. Returns `{ installed: bool, path? }` |
| `POST` | `/api/speedtest/run` | Start an async test. Body: see below. Returns **201** `{ jobId }` immediately — the test runs in the background |
| `GET` | `/api/speedtest/result/:jobId` | Poll a job. Returns a `SpeedtestRecord` (see below); `status` is `"running"`, `"done"`, or `"error"` |
| `GET` | `/api/speedtest/results` | Full history (last 100 runs). Returns `{ results: [SpeedtestRecord, ...] }` |
| `DELETE` | `/api/speedtest/results` | Clear history |
| `POST` | `/api/speedtest/server` | *Internal.* Start an `iperf3 -s --one-off` on this server. Returns `{ port, sessionId }` |
| `DELETE` | `/api/speedtest/server/:sessionId` | *Internal.* Kill a running `iperf3` server session |
| `POST` | `/api/speedtest/client` | *Internal.* Run `iperf3 -c` against a given host/port and return the result |

**POST /api/speedtest/run — body:**

| Field | Type | Description |
|-------|------|-------------|
| `fromServer`, `toServer` | string | Display names, stored with the result for history readability |
| `fromRemoteId`, `toRemoteId` | string | `""` = local server, else a Remote ID — determines which side runs `iperf3 -s` vs `-c` |
| `host` | string | IP/hostname the `iperf3` client connects to (required) |
| `bindAddr` | string | Optional: bind the client to a specific local IP (e.g. a tunnel interface IP, for a tunnel-mode test) |
| `via` | string | `"tunnel"` or `"internet"` — informational only, stored with the result; does not change the test itself beyond `host`/`bindAddr` |
| `duration` | int | Seconds, default 10 |
| `streams` | int | Parallel TCP streams (`iperf3 -P`), default 4 |

**SpeedtestRecord fields:** `id, fromServer, toServer, host, port, duration, streams, status, via,
sendMbps, recvMbps, retransmits, latencyMs, error, startedAt, finishedAt`. The `Mbps`/
`retransmits`/`latencyMs` fields are `null` until `status` is `"done"`.

> The test always runs plain TCP (`iperf3` with no `-u`) regardless of `via` — when `via:
> "tunnel"`, the WireGuard/AmneziaWG UDP encapsulation still applies underneath, so provider-side
> UDP shaping can make a tunnel-mode result much lower than an internet-mode one on the exact same
> pair of servers. This is a real network-path effect, not a bug — see the troubleshooting note in
> the README if you hit an unexpectedly large gap between the two.

---

## System Backup

### Create Backup

```
POST /api/system/backup
Content-Type: application/json
Authorization: Bearer ws_...

{ "password": "optional" }
```

| Field | Type | Description |
|-------|------|-------------|
| `password` | string | Optional. If provided — file is encrypted with AES-256-GCM. Empty string or absent — no encryption. |

**Response:** binary stream (file download).

| Password | Filename | Content-Type |
|----------|----------|--------------|
| Not set | `cascade-backup-YYYYMMDD-HHMMSS.tar.gz` | `application/gzip` |
| Set | `cascade-backup-YYYYMMDD-HHMMSS.tar.gz.enc` | `application/octet-stream` |

Archive contents: `awg.db` + `*.save` (ipset files).

**Examples (curl):**

```bash
# Without password
curl -X POST https://<host>/<admin_path>/api/system/backup \
  -H "Authorization: Bearer ws_..." \
  -H "Content-Type: application/json" \
  -d '{}' \
  -o cascade-backup.tar.gz

# With password (encrypted)
curl -X POST https://<host>/<admin_path>/api/system/backup \
  -H "Authorization: Bearer ws_..." \
  -H "Content-Type: application/json" \
  -d '{"password": "mypassword"}' \
  -o cascade-backup.tar.gz.enc
```

### Preview a Restore

```
POST /api/system/restore/preview
Content-Type: multipart/form-data
Authorization: Bearer ws_...
```

| Field | Type | Description |
|-------|------|-------------|
| `backup` | file | `.tar.gz` or `.tar.gz.enc` backup file |
| `password` | string | Required if file is encrypted, otherwise — `400` |

Inspects the backup's DB (without touching current state) and compares the physical interface
names referenced by its NAT rules against this server's actual interfaces — useful when
restoring a backup taken on a different machine where `eth0`/`ens3`/etc. may not match.

**Response (200):** `{ "backupIfaces": [...], "serverIfaces": [...], "needsRemap": bool }`

If `needsRemap` is `true`, pass an `ifaceMap` (e.g. `{"eth0":"ens3"}`) to `POST /restore` below
to rewrite `out_interface` in the restored NAT rules.

### Restore from Backup

```
POST /api/system/restore
Content-Type: multipart/form-data
Authorization: Bearer ws_...
```

| Field | Type | Description |
|-------|------|-------------|
| `backup` | file | `.tar.gz` or `.tar.gz.enc` backup file |
| `password` | string | Required if file is encrypted, otherwise — `400` |
| `ifaceMap` | string (JSON) | Optional. e.g. `{"eth0":"ens3"}` — remaps `out_interface` in the restored `nat_rules` table. See "Preview a Restore" above |

**Response (200):** `{ "message": "Backup restored. Container is restarting…", "restored": N }`

Restore flow: auto-backs up current state to `data/pre-restore-<timestamp>.tar.gz` first, stops
all WireGuard interfaces, flushes firewall chains and ipsets, writes the backup's files over the
current data directory, removes stale WAL/SHM files, applies `ifaceMap` if given, then exits the
process — Docker's `restart: always` brings it back up with the restored state.

**Errors:**
- `400 "this backup is encrypted — provide the password"` — encrypted file with no password
- `400 "wrong password or corrupted backup file"` — wrong password (data untouched)

After a successful restore, the process exits after 300 ms — Docker restarts the container (`restart: always`).

**Examples (curl):**

```bash
# Unencrypted
curl -X POST https://<host>/<admin_path>/api/system/restore \
  -H "Authorization: Bearer ws_..." \
  -F "backup=@cascade-backup.tar.gz"

# Encrypted
curl -X POST https://<host>/<admin_path>/api/system/restore \
  -H "Authorization: Bearer ws_..." \
  -F "backup=@cascade-backup.tar.gz.enc" \
  -F "password=mypassword"
```

### List Pre-Restore Auto-Backups

```
GET /api/system/backups
```

Every `POST /restore` automatically snapshots current state to `data/pre-restore-<timestamp>.tar.gz`
before overwriting anything (see above) — this lists those safety snapshots.

**Response (200):** `{ "backups": [{ "name": "pre-restore-20260115-030405.tar.gz", "size": 123456, "createdAt": "2026-01-15T03:04:05Z" }, ...] }`

These files are **not** downloadable via the API — restore one manually from the server's
data directory if needed (`docker exec cascade ls /etc/wireguard/data/`).

### Automated Backup (cron)

```bash
#!/bin/bash
# /etc/cron.daily/cascade-backup
DATE=$(date +%Y%m%d-%H%M%S)
DEST="/var/backups/cascade"
mkdir -p "$DEST"

curl -sf -X POST https://<host>/<admin_path>/api/system/backup \
  -H "Authorization: Bearer ws_..." \
  -H "Content-Type: application/json" \
  -d '{"password": "your-backup-password"}' \
  -o "$DEST/cascade-$DATE.tar.gz.enc"

# Delete backups older than 30 days
find "$DEST" -name "*.tar.gz.enc" -mtime +30 -delete
```

---

## Compatibility Stubs

Legacy endpoints retained for frontend compatibility. Read-only, return safe defaults.

### Unauthenticated

| Method | Path | Returns |
|--------|------|---------|
| `GET` | `/api/lang` | `"en"` |
| `GET` | `/api/release` | `999999` (suppresses update banner) |
| `GET` | `/api/remember-me` | `true` |
| `GET` | `/api/ui-traffic-stats` | `false` |
| `GET` | `/api/ui-chart-type` | `0` |
| `GET` | `/api/wg-enable-one-time-links` | `true` |
| `GET` | `/api/ui-sort-clients` | `false` |
| `GET` | `/api/wg-enable-expire-time` | `false` |
| `GET` | `/api/ui-avatar-settings` | `{ dicebear: null, gravatar: false }` |

### Authenticated

| Method | Path | Returns |
|--------|------|---------|
| `GET` | `/api/wireguard/client` | `[]` — admin tunnel not yet implemented |
| `ALL` | `/api/wireguard/*` | `501 Not Implemented` |
| `GET` | `/api/system/interfaces` | `{ interfaces: [...] }` — host interfaces |

---

## Response Conventions

- All list endpoints return a **named wrapper**: `{ peers/interfaces/rules/routes/... : [...] }` — never a bare array
- Errors: `{ error: "message" }` with appropriate HTTP status (400 / 401 / 404 / 500)
- Toggle via PATCH: `{ enabled: true|false }` — no other fields required
- Timestamps: RFC3339 UTC — `"2026-03-19T10:00:00Z"`
- Interface IDs: string slugs — `"wg10"`, `"wg11"`, …
- All other IDs: UUID v4
