#!/bin/bash
# First-time setup: install acme.sh and issue a short-lived Let's Encrypt
# certificate for a bare public IP address.
#
# Usage:
#   sudo ./acme-install.sh <PUBLIC_IP> <EMAIL>
#
# What it does:
#   1. Installs acme.sh (if not present)
#   2. Sets RENEW_DAYS=1 so 6-day shortlived certs renew on day 5 (not day 30+)
#   3. Issues a shortlived cert (6 days) for the IP via HTTP-01 standalone mode
#      (acme.sh binds a temporary HTTP server on port 80 — no Caddy needed yet)
#   4. Installs the cert to /etc/ssl/cascade/ with reloadcmd = docker restart cascade-caddy
#      NOTE: "admin off" in Caddyfile disables the admin API — caddy reload fails,
#            docker restart is the only way to reload the cert.
#   5. Starts Caddy (docker compose up -d)
#   6. Re-issues cert via webroot mode to set Le_Webroot='/srv/acme' in acme.sh config.
#      Without this step, Le_Webroot='no' (standalone) is remembered and renewals
#      would try to bind port 80 again — conflicting with running Caddy.
#
# Requirements:
#   - Port 80 must be reachable from the internet during FIRST issuance
#   - Run from deploy/caddy/ directory (docker compose must be available)
#   - .env must exist with ADMIN_PATH and CASCADE_PORT set
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

# Set RENEW_DAYS=1 globally.
# Default is 30 days, but shortlived certs live only 6 days.
# With RENEW_DAYS=1: renewal happens when 1 day remains (day 5 of 6) — correct.
# Without this: acme.sh cron either renews every run or never, depending on version.
echo "==> Setting RENEW_DAYS=1 for shortlived certs..."
if grep -q "^RENEW_DAYS=" "$HOME/.acme.sh/account.conf" 2>/dev/null; then
    sed -i 's/^RENEW_DAYS=.*/RENEW_DAYS=1/' "$HOME/.acme.sh/account.conf"
else
    echo 'RENEW_DAYS=1' >> "$HOME/.acme.sh/account.conf"
fi

# Step 1: First issuance via standalone mode.
# acme.sh temporarily binds port 80 to answer the ACME HTTP-01 challenge.
# Caddy is not running yet — this is intentional (chicken-and-egg: no cert → can't start Caddy).
echo "==> Issuing short-lived certificate for $IP (standalone mode)..."
~/.acme.sh/acme.sh \
    --issue \
    --server letsencrypt \
    -d "$IP" \
    --standalone \
    --cert-profile shortlived \
    --days 1

# Step 2: Install cert.
# reloadcmd = docker restart (NOT caddy reload — admin off disables the admin API).
echo "==> Installing certificate to $CERT_DIR..."
~/.acme.sh/acme.sh \
    --install-cert -d "$IP" --ecc \
    --key-file       "$CERT_DIR/server.key" \
    --fullchain-file "$CERT_DIR/server.crt" \
    --reloadcmd      "docker restart $CADDY_CONTAINER"

chmod 600 "$CERT_DIR/server.key"
chmod 644 "$CERT_DIR/server.crt"

# Step 3: Start Caddy.
# Cert is now available → Caddy can start and will serve /.well-known/acme-challenge/*
# from /srv/acme (configured in Caddyfile).
echo "==> Starting Caddy..."
docker compose up -d
echo "==> Waiting for Caddy to start..."
sleep 3

# Step 4: Re-issue via webroot to update Le_Webroot in acme.sh config.
# After standalone issue, Le_Webroot='no' is saved. If left as-is, future renewals
# would try standalone again → port 80 conflict with running Caddy → renewal fails.
# --force re-issues immediately and saves Le_Webroot='/srv/acme' for all future renewals.
echo "==> Switching to webroot mode for future renewals..."
~/.acme.sh/acme.sh \
    --issue \
    --server letsencrypt \
    -d "$IP" \
    --webroot "$ACME_WEBROOT" \
    --cert-profile shortlived \
    --days 1 \
    --force

echo ""
echo "Done. Certificate installed to $CERT_DIR"
echo ""
echo "Renewal schedule:"
echo "  cron: $(crontab -l 2>/dev/null | grep acme || echo 'not found — run: acme.sh --install-cronjob')"
echo "  next: $(grep Le_NextRenewTimeStr "$HOME/.acme.sh/${IP}_ecc/${IP}.conf" 2>/dev/null || echo 'check ~/.acme.sh/')"
echo ""
echo "Access Cascade at: https://$IP/<ADMIN_PATH>/"
