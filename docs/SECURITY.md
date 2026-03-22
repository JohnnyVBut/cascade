# WireSteer — Security Model

## Defense-in-depth overview

```
Internet
   │
   ▼
[Caddy reverse proxy]
   • TLS (Let's Encrypt, auto-renew)
   • Secret ADMIN_PATH prefix — unknown to attacker
   • Rate limit: 5 POST/min per IP
   • Referrer-Policy: no-referrer (admin URL doesn't leak via Referer header)
   • Decoy site (StreamVault) served for all other paths
   │
   ▼ only /<ADMIN_PATH>/* reaches WireSteer
[WireSteer on 127.0.0.1:8888]
   • Password authentication (bcrypt)
   • TOTP 2FA (per-user, optional but recommended)
   • Session cookie (SameSite=Strict)
   • API tokens for programmatic access (SHA-256 stored, shown once)
```

---

## Threat model

### Admin URL discovery

| Attack | Feasible? | Notes |
|--------|-----------|-------|
| Wordlist fuzzing (gobuster, ffuf) | ❌ No | Random 32-char hex won't be in any wordlist |
| Brute-force enumeration | ❌ No | 2¹²⁸ search space — longer than age of universe |
| Referrer header leak | ✅ Mitigated | `Referrer-Policy: no-referrer` prevents URL from leaking |
| Browser history / screenshot | ⚠️ Low | Operational security, not mitigatable by software |
| Server access log compromise | ⚠️ Low | Logs require root access |

**Recommendation:** Use a random hex string (`openssl rand -hex 16`) as ADMIN_PATH.
Human-readable phrases are vulnerable to wordlist and rule-based attacks.

### Login brute-force (if admin URL leaks)

| Layer | Protection |
|-------|-----------|
| Password (bcrypt, cost 12) | ~100ms/attempt server-side |
| Rate limit (5 POST/min/IP) | Max ~7200 attempts/day per IP |
| TOTP (30s window) | Attacker needs physical TOTP device |

With TOTP enabled: even knowing the URL + password, the attacker needs the current
TOTP code (valid 30s). Brute-forcing 6 digits within 30 seconds against a rate-limited
endpoint is infeasible.

**Remaining gap:** No IP ban after N failed attempts. See roadmap below.

---

## Vulnerability assessment

### ✅ SQL Injection — not vulnerable

All database queries use parameterized placeholders (`?`). No string concatenation
in SQL statements anywhere in the codebase.

```go
// Example — correct pattern used throughout:
db.QueryRow("SELECT * FROM users WHERE username = ?", username)
```

### ✅ XSS (Cross-Site Scripting) — not vulnerable

Frontend is Vue 2 SPA. All template interpolations (`{{ }}`) are auto-escaped.
`v-html` is never used with user-controlled content.

### ✅ Path Traversal — not vulnerable

Config file paths are constructed from interface IDs sourced from the database,
not from HTTP request parameters directly.

### ✅ SameSite cookie — fixed (v3.1)

Session cookie is set with `SameSite=Strict`, preventing cross-site request
forgery in all modern browsers.

### ⚠️ Command Injection — partially mitigated

`Util.exec()` runs shell commands via `bash`. Interface IDs and names are passed
into shell commands (e.g. `awg-quick up wg10`).

**Mitigation in place:** Interface IDs are auto-assigned (`wg10`, `wg11`, …) and
validated against `^[a-zA-Z0-9_-]{1,15}$` before any shell invocation.
Interface display names are never passed to shell commands.

**Residual risk:** Peer public keys and PSK are passed to `wg set` commands.
These are validated as base64 strings before use.

### ⚠️ SSRF (Server-Side Request Forgery) — partial risk

Gateway monitor makes outbound HTTP/ICMP requests to user-specified addresses.
An authenticated admin could specify an internal RFC-1918 address to probe the
local network.

**Current state:** No RFC-1918 blocking implemented.

**Impact:** Limited — requires authenticated admin access. Not exploitable by
unauthenticated attackers.

**Roadmap:** Block RFC-1918 and link-local ranges in gateway monitor addresses.

### ⚠️ Denial of Service — partial mitigation

- Request body size: not explicitly limited (Fiber default: 4MB)
- `/api/aliases/:id/generate` triggers external HTTP requests to RIPE NCC/ipdeny
  — repeated calls could be slow or rate-limited by upstream
- No per-connection limit beyond Caddy's rate limiting

**Roadmap:** Explicit `BodyLimit` in Fiber config; debounce on generate endpoint.

### ✅ CSRF (Cross-Site Request Forgery) — mitigated

Session cookies use `SameSite=Strict`. CSRF tokens are not implemented, but
`SameSite=Strict` prevents cross-origin cookie submission in all modern browsers.

---

## Security hardening checklist

### Deployment

- [ ] Use random hex ADMIN_PATH (`openssl rand -hex 16`)
- [ ] Set `BIND_ADDR=127.0.0.1` (WireSteer not exposed to internet)
- [ ] Enable TOTP for all admin accounts
- [ ] Use strong password (16+ chars, mixed)
- [ ] Keep acme.sh cert auto-renewal working (check crontab)
- [ ] Restrict SSH access (key-only, non-standard port)
- [ ] Keep Docker and host OS updated

### Optional hardening

- **IP allowlist in Caddy:** Restrict admin access to specific IPs or WireGuard subnet:
  ```caddy
  @admin {
      path /{$ADMIN_PATH}/*
      remote_ip 10.0.0.0/8 192.168.0.0/16
  }
  ```
- **Fail2ban:** Block IPs after repeated failed login attempts (see roadmap)

---

## API token security

- Format: `ws_` + 64 hex chars (256 bits of entropy)
- Storage: only SHA-256 hash stored in database — raw token shown once at creation
- Tokens bypass TOTP by design (for scripting/automation)
- Tokens are scoped to the creating user
- Instant revocation via UI or `DELETE /api/tokens/:id`
- `last_used` timestamp updated on every authenticated request

**Recommendation:** Treat API tokens as passwords. Store in a secrets manager
or environment variable, never in plaintext files or version control.

---

## Incident response

### If ADMIN_PATH leaks

1. Change ADMIN_PATH in `deploy/.env`
2. Restart Caddy: `docker compose -f deploy/caddy/docker-compose.yml restart`
3. Old path becomes the decoy — no access possible

### If password is compromised

1. Log in and change password immediately (Settings → Users → Edit)
2. Revoke all active API tokens
3. Check login history (currently: not implemented — see roadmap)
4. Re-generate TOTP secret if device may be compromised

### If API token leaks

1. Go to Settings → API Tokens → Revoke immediately
2. Token is invalidated on the next request
3. Review what the token was used for (check `last_used` timestamp)

---

## Roadmap

| Priority | Feature | Description |
|----------|---------|-------------|
| 🔴 High | Fail2ban / IP lockout | Block IP after N failed login attempts (e.g. 10 attempts → 15 min ban). Store attempt counter in SQLite. |
| 🟡 Medium | SSRF mitigation | Reject RFC-1918, loopback, and link-local IPs in gateway monitor address field |
| 🟡 Medium | Login audit log | Record login attempts (timestamp, IP, success/failure) — visible in UI |
| 🟡 Medium | Request body limit | Explicit `BodyLimit: 1MB` in Fiber to prevent memory exhaustion |
| 🟢 Low | IP allowlist UI | Configure allowed source IPs for admin access from within WireSteer settings |
| 🟢 Low | Telegram alerts | Notify on failed login burst, ADMIN_PATH change, new API token created |
