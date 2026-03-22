# Cascade — Caddy Reverse Proxy

Caddy sits in front of Cascade and provides:
- HTTPS + HTTP/3 (QUIC) on port 443
- Decoy streaming site on `/`
- Hidden admin path (configured via `ADMIN_PATH` env var)
- Rate limiting on login endpoint (5 attempts / IP / minute)
- Security headers (HSTS, no-referrer, X-Frame-Options, …)
- TLS cert for bare public IP via acme.sh shortlived profile

## Quick start

### 1. Issue TLS certificate

```bash
# Port 80 must be reachable from the internet (acme.sh standalone mode binds it briefly).
# No existing HTTP server needed — this is designed to run BEFORE Caddy starts.
chmod +x scripts/acme-install.sh
sudo ./scripts/acme-install.sh <YOUR_PUBLIC_IP> <YOUR_EMAIL>
```

### 2. Configure

```bash
cp .env.example .env
# Edit .env — set ADMIN_PATH to a random string
# Generate one: openssl rand -hex 12
```

### 3. Add decoy video

Download Big Buck Bunny (or any neutral mp4) to:
```
www/video/decoy.mp4
```

### 4. Ensure Cascade binds to 127.0.0.1 only

Set `BIND_ADDR=127.0.0.1` in `docker-compose.go.yml` (already the default).
This prevents Cascade from being reachable directly from the internet — all traffic
must go through Caddy's hidden `ADMIN_PATH`.

As a second layer, block the port via iptables:
```bash
iptables-nft -A INPUT ! -i lo -p tcp --dport 8888 -j DROP
```

### 5. Start Caddy

```bash
docker compose up -d --build
```

### 6. Access admin interface

```
https://<IP>/<ADMIN_PATH>/
```

## Security notes

- `ADMIN_PATH` is security through obscurity — TOTP in Cascade is the real gate
- `Referrer-Policy: no-referrer` prevents the hidden path from leaking via Referer headers
- Rate limiting blocks brute force on the login endpoint (5 POST /api/session per IP per minute)
- Cascade port (default 8888) MUST NOT be reachable from the internet (see step 4)
- TLS cert renews automatically every 3 days via acme.sh cron (webroot via Caddy after first issue)
- Caddy container runs read-only with minimal capabilities (NET_BIND_SERVICE only)

## Certificate issuance model

**First issuance** (`acme-install.sh`): uses acme.sh `--standalone` mode.
acme.sh temporarily binds port 80, answers the ACME HTTP-01 challenge, then exits.
No existing HTTP server required — this is intentional (Caddy can't start without a cert).

**Renewals** (automatic, every 3 days via cron): acme.sh switches to webroot mode,
placing the challenge token in `/srv/acme`. Caddy serves `/.well-known/acme-challenge/*`
from that directory (configured in `Caddyfile`), then gets reloaded automatically.

```
First time:  acme.sh --standalone   (binds :80 itself, no Caddy needed)
Renewals:    acme.sh --webroot /srv/acme  (Caddy serves the challenge)
```
