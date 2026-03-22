#!/bin/bash
# First-time setup: install acme.sh and issue a short-lived Let's Encrypt
# certificate for a bare public IP address.
#
# Usage:
#   sudo ./acme-install.sh <PUBLIC_IP> <EMAIL>
#
# What it does:
#   1. Installs acme.sh (if not present)
#   2. Issues a shortlived cert (6 days) for the IP via HTTP-01 standalone mode
#      (acme.sh binds a temporary HTTP server on port 80 — no Caddy needed yet)
#   3. Installs the cert to /etc/ssl/cascade/
#   4. Configures auto-renewal via acme.sh cron (every 3 days)
#   5. On renewal: Caddy serves /.well-known/acme-challenge/* via webroot /srv/acme
#      and reloads automatically via `docker exec cascade-caddy caddy reload`
#
# Requirements:
#   - Port 80 must be reachable from the internet during FIRST issuance
#     (acme.sh standalone mode binds port 80 briefly; no existing HTTP server needed)
#   - For RENEWAL: Caddy handles the challenge via webroot /srv/acme (already configured)
#   - Caddy container name: cascade-caddy (see docker-compose.yml)
#   - Docker must be installed and running

set -euo pipefail

IP="${1:?Usage: $0 <PUBLIC_IP> <EMAIL>}"
EMAIL="${2:?Usage: $0 <PUBLIC_IP> <EMAIL>}"

CERT_DIR="/etc/ssl/cascade"
ACME_WEBROOT="/srv/acme"
CADDY_CONTAINER="cascade-caddy"

echo "==> Creating directories..."
mkdir -p "$CERT_DIR" "$ACME_WEBROOT"
chmod 755 "$ACME_WEBROOT"
chmod 700 "$CERT_DIR"

# Install acme.sh if not already present
if [ ! -f "$HOME/.acme.sh/acme.sh" ]; then
    echo "==> Installing acme.sh..."
    curl https://get.acme.sh | sh -s email="$EMAIL"
    # shellcheck disable=SC1090
    source "$HOME/.acme.sh/acme.sh.env"
else
    echo "==> acme.sh already installed, skipping"
    # shellcheck disable=SC1090
    source "$HOME/.acme.sh/acme.sh.env" 2>/dev/null || true
fi

# First issuance: standalone mode — acme.sh temporarily binds port 80 itself.
# This avoids the chicken-and-egg problem (Caddy not yet started, cert not yet available).
# On subsequent renewals, acme.sh switches to webroot (/srv/acme) served by Caddy.
echo "==> Issuing short-lived certificate for $IP (standalone mode)..."
~/.acme.sh/acme.sh \
    --issue \
    --server letsencrypt \
    -d "$IP" \
    --standalone \
    --certificate-profile shortlived \
    --days 3

echo "==> Installing certificate to $CERT_DIR..."
~/.acme.sh/acme.sh \
    --install-cert -d "$IP" \
    --key-file       "$CERT_DIR/server.key" \
    --fullchain-file "$CERT_DIR/server.crt" \
    --reloadcmd      "docker exec $CADDY_CONTAINER caddy reload --config /etc/caddy/Caddyfile"

chmod 600 "$CERT_DIR/server.key"
chmod 644 "$CERT_DIR/server.crt"

echo ""
echo "Done. Certificate installed to $CERT_DIR"
echo "Auto-renewal is configured via acme.sh cron (runs every 3 days)."
echo ""
echo "Verify renewal cron:"
echo "  crontab -l | grep acme"
