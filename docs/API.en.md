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

## Settings

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/settings` | Global settings |
| `PUT` | `/api/settings` | Partial update. Body: `{ dns?, defaultPersistentKeepalive?, defaultClientAllowedIPs?, gatewayWindowSeconds?, gatewayHealthyThreshold?, gatewayDegradedThreshold? }` |

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
| `POST` | `/api/templates/generate` | Generate AWG2 params. Body: `{ profile, intensity, host?, browser?, saveName? }`. browser: chrome|firefox|safari|edge|yandex_desktop|yandex_mobile |

---

## Tunnel Interfaces

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/tunnel-interfaces` | List interfaces. Returns `{ interfaces: [...] }` |
| `POST` | `/api/tunnel-interfaces` | Create. Body: `{ name, address, listenPort, protocol, disableRoutes?, settings? }` |
| `POST` | `/api/tunnel-interfaces/quick-create` | Quick-create: create and start a client interface in one step. Body: `{ name?: string, protocol?: string }`. Address and port are auto-assigned from SubnetPool/PortPool settings. AWG2 params come from the default template or a random profile. Response: `{ interface, started: bool, startError?: string }` |
| `GET` | `/api/tunnel-interfaces/:id` | Get interface |
| `PATCH` | `/api/tunnel-interfaces/:id` | Update (hot-reload via syncconf). Body: partial fields |
| `DELETE` | `/api/tunnel-interfaces/:id` | Delete interface |
| `POST` | `/api/tunnel-interfaces/:id/start` | Start. Returns `{ interface }` |
| `POST` | `/api/tunnel-interfaces/:id/stop` | Stop. Returns `{ interface }` |
| `POST` | `/api/tunnel-interfaces/:id/restart` | Restart. Returns `{ interface }` |
| `GET` | `/api/tunnel-interfaces/:id/export-params` | S2S export. Returns `{ name, publicKey, endpoint, address, protocol, presharedKey? }` |
| `GET` | `/api/tunnel-interfaces/:id/export-obfuscation` | AWG2 obfuscation params as JSON |
| `GET` | `/api/tunnel-interfaces/:id/backup` | Download interface + all peers as JSON |
| `PUT` | `/api/tunnel-interfaces/:id/restore` | Restore peers from backup. Removes existing peers first |

---

## Peers

Base path: `/api/tunnel-interfaces/:id/peers`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/peers` | List peers. Returns `{ peers: [...] }` |
| `POST` | `/peers` | Create peer. Body: `{ name, peerType (client/interconnect), clientAllowedIPs?, persistentKeepalive?, expiredAt? }` |
| `POST` | `/peers/import-json` | Create interconnect peer from exported JSON |
| `GET` | `/peers/:peerId` | Get peer |
| `PATCH` | `/peers/:peerId` | Update peer fields |
| `DELETE` | `/peers/:peerId` | Delete peer |
| `GET` | `/peers/:peerId/config` | Download WireGuard config file |
| `GET` | `/peers/:peerId/qrcode.svg` | QR code SVG (client peers only) |
| `POST` | `/peers/:peerId/enable` | Enable peer |
| `POST` | `/peers/:peerId/disable` | Disable peer |
| `PUT` | `/peers/:peerId/name` | Rename peer. Body: `{ name }` |
| `PUT` | `/peers/:peerId/address` | Update overlay address. Body: `{ address }` → stored as AllowedIPs |
| `PUT` | `/peers/:peerId/expireDate` | Set expiry. Body: `{ expireDate }` — RFC3339 or YYYY-MM-DD, empty clears |
| `POST` | `/peers/:peerId/generateOneTimeLink` | Generate one-time config link token |
| `GET` | `/peers/:peerId/export-json` | Export interconnect peer as JSON (interconnect only) |

---

## Routing

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/routing/table` | Kernel routes. Query: `?table=main` (default) |
| `GET` | `/api/routing/tables` | Routing tables from `ip rule show`. Returns `{ tables: [...] }` |
| `GET` | `/api/routing/test` | Route lookup. Query: `?ip=<dst>[&src=<src>][&mark=<fwmark>]`. With `src`: SimulateTrace (PBR) → `ip route get <dst> mark <fwmark>`. Returns `{ result, matchedRule, steps }` |
| `GET` | `/api/routing/routes` | Static routes (DB). Returns `{ routes: [...] }` |
| `POST` | `/api/routing/routes` | Create static route. Body: `{ destination, via?, dev?, metric?, table?, comment? }` |
| `PATCH` | `/api/routing/routes/:id` | Update or toggle: `{ enabled: bool }` |
| `DELETE` | `/api/routing/routes/:id` | Delete route |

---

## NAT

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/nat/interfaces` | Host network interfaces. Returns `{ interfaces: [...] }` |
| `GET` | `/api/nat/rules` | NAT rules + auto-rules from tunnel interfaces. Returns `{ rules: [...] }`. Auto-rules have `"auto": true` (read-only) |
| `POST` | `/api/nat/rules` | Create rule. Body: `{ name, source?, sourceAliasId?, outInterface, type (MASQUERADE/SNAT), toSource? (SNAT only), comment? }` |
| `PATCH` | `/api/nat/rules/:id` | Update or toggle: `{ enabled: bool }` |
| `DELETE` | `/api/nat/rules/:id` | Delete rule |

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
| `POST` | `/api/firewall/rules/:id/move` | Reorder. Body: `{ direction: "up"\|"down" }` |

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
| `port` | `["tcp:443", "udp:53", "any:80"]` | L4 ports |
| `port-group` | `["<portAliasId>"]` | Combines port aliases |

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
| `GET` | `/api/wg-enable-one-time-links` | `false` |
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
