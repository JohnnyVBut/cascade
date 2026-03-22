# WireSteer — Deploy from Scratch

Full server setup: Ubuntu 22.04 / 24.04 + AmneziaWG kernel module + Docker + Caddy reverse proxy.

---

## Requirements

| | |
|---|---|
| OS | Ubuntu 22.04 LTS or Ubuntu 24.04 LTS |
| Kernel | 6.1+ (see Step 1) |
| RAM | 512 MB minimum |
| Access | Root |
| Network | Public IP, ports 443/TCP and WireGuard UDP ports open |

---

## Step 1 — Upgrade kernel to 6.x (Ubuntu 22.04 only)

> **Ubuntu 24.04:** skip this step — ships with kernel 6.8 by default.

Ubuntu 22.04 ships with kernel 5.15. The AmneziaWG DKMS module requires ≥ 6.1
(`timer_delete` symbol). Install the HWE kernel:

```bash
apt update && apt install -y linux-generic-hwe-22.04
reboot
```

After reboot verify:

```bash
uname -r
# expected: 6.8.x-xx-generic
```

---

## Step 2 — Install AmneziaWG kernel module

```bash
add-apt-repository ppa:amnezia/ppa
apt install -y amneziawg
```

Load the module immediately and register it for autoload on boot:

```bash
modprobe amneziawg
echo "amneziawg" | tee /etc/modules-load.d/amneziawg.conf
```

Verify:

```bash
lsmod | grep amneziawg
# expected: amneziawg   131072  0
```

---

## Step 3 — Install Docker

```bash
curl -fsSL https://get.docker.com | sh
```

---

## Step 4 — Configure kernel parameters

Enable IP forwarding and tune network buffers (required for WireGuard routing and HTTP/3):

```bash
cat > /etc/sysctl.d/99-wiresteer.conf << 'EOF'
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
net.core.rmem_max = 7340032
net.core.wmem_max = 7340032
EOF

sysctl --system
```

---

## Step 5 — Clone repository

```bash
git clone https://github.com/JohnnyVBut/awg-easy.git
cd awg-easy
git checkout feature/go-rewrite
```

---

## Step 6 — Build WireSteer (Go binary + Docker image)

```bash
./build-go.sh
```

The script compiles the Go binary and builds the Docker image `awg2-easy-go:latest`.

---

## Step 7 — Configure WireSteer

Edit `docker-compose.go.yml`. The key variables:

```yaml
environment:
  - PASSWORD_HASH=          # bcrypt hash of your admin password (see below)
  - WG_HOST=                # your server's public IP or hostname
  - PORT=8888               # Web UI port (listens on localhost only)
  - BIND_ADDR=127.0.0.1     # bind to localhost — Caddy proxies from outside
```

**Generate password hash:**

```bash
docker run --rm -it awg2-easy-go:latest /app/wiresteer hash
# Enter password when prompted — copy the $2a$... hash
```

Paste the hash as the value of `PASSWORD_HASH=`.

---

## Step 8 — Start WireSteer

```bash
docker compose -f docker-compose.go.yml up -d
```

Verify it is healthy and listening on localhost:

```bash
docker ps
curl http://127.0.0.1:8888/api/health
# expected: {"host":"...","status":"ok","version":"3.0.0-alpha"}
```

---

## Step 9 — Obtain TLS certificate (acme.sh)

WireSteer must be running (Step 8) before this step.
acme.sh uses standalone mode — it temporarily binds port 80 to complete the ACME HTTP-01 challenge.
**Port 80 must be free** (Caddy is not started yet at this point).

Install acme.sh:

```bash
curl https://get.acme.sh | sh -s email=YOUR@EMAIL.COM
source ~/.bashrc
```

### Option A — bare IP address (most common for VPS)

Let's Encrypt supports TLS certificates for bare IP addresses, but **only** via the
`shortlived` profile (6-day validity). Standard 90-day certificates for IPs are not
available from Let's Encrypt.

```bash
~/.acme.sh/acme.sh --issue \
  --server letsencrypt \
  -d YOUR.SERVER.IP \
  --standalone \
  --certificate-profile shortlived \
  --days 3
```

acme.sh installs a cron job that renews automatically every 3 days — no manual action needed.

### Option B — domain name

If you have a domain pointing to the server, use a standard 90-day certificate instead:

```bash
~/.acme.sh/acme.sh --issue \
  --server letsencrypt \
  -d yourdomain.example.com \
  --standalone
```

No `--certificate-profile` flag needed. Auto-renewal every 60 days.

Install the certificate to a persistent location:

```bash
mkdir -p /etc/ssl/wiresteer

~/.acme.sh/acme.sh --install-cert -d YOUR.SERVER.IP \
  --key-file       /etc/ssl/wiresteer/server.key \
  --fullchain-file /etc/ssl/wiresteer/server.crt \
  --reloadcmd      "docker exec wiresteer-caddy caddy reload --config /etc/caddy/Caddyfile 2>/dev/null || true"
```

---

## Step 10 — Deploy Caddy reverse proxy

Caddy sits in front of WireSteer: serves the decoy site on HTTPS, and only routes
requests under a secret path to the admin UI.

```bash
cd ~/awg-easy/deploy/caddy
cp .env.example .env
```

Edit `.env`:

```bash
# Secret path prefix for the admin UI — choose something random, no slashes
ADMIN_PATH=your_random_secret_here

# WireSteer port (must match PORT in docker-compose.go.yml)
WIRESTEER_PORT=8888
```

Start Caddy:

```bash
docker compose up -d --build
docker compose logs -f
```

Expected output — no errors, then:
```
{"level":"info","msg":"serving initial configuration"}
```

---

## Step 11 — Verify full stack

```bash
# Decoy site (no admin path)
curl -k https://YOUR.SERVER.IP
# → StreamVault HTML

# Admin UI (with secret path — note the trailing slash)
curl -k https://YOUR.SERVER.IP/YOUR_ADMIN_PATH/api/health
# → {"status":"ok",...}
```

Open in browser: `https://YOUR.SERVER.IP/YOUR_ADMIN_PATH/`

---

## Ports reference

| Port | Protocol | Purpose |
|------|----------|---------|
| 443 | TCP + UDP (HTTP/3) | HTTPS — Caddy (public) |
| 80 | TCP | ACME renewal only (not permanently open) |
| 8888 | TCP | WireSteer UI — bound to 127.0.0.1, not public |
| 51830 | UDP | WireGuard interface wg10 (first tunnel) |
| 51831 | UDP | WireGuard interface wg11, etc. |

WireGuard UDP ports must be open in the host firewall:

```bash
ufw allow 51830:51840/udp
```

---

## Data directory

All WireSteer state is stored in `~/awg-easy/data/`:

```
data/
  wireguard.db          ← SQLite: interfaces, peers, routes, NAT, firewall rules, etc.
  *.save                ← ipset snapshots (auto-restored on startup)
  /etc/amnezia/amneziawg/wg10.conf   ← generated WireGuard configs (inside container)
```

The data directory is mounted into the container via `docker-compose.go.yml`.

---

## Updating

```bash
cd ~/awg-easy
git pull origin feature/go-rewrite
./build-go.sh
docker compose -f docker-compose.go.yml up -d
```

Caddy does not need to be restarted for WireSteer updates.

---

## Troubleshooting

### AmneziaWG module not loaded after reboot

```bash
modprobe amneziawg
# If that fails — check DKMS build status:
dkms status
uname -r        # must be 6.x
```

### WireSteer container exits immediately

```bash
docker logs awg-router
```

Common causes:
- `PASSWORD_HASH` is empty or malformed
- `WG_HOST` is not set

### Interfaces not appearing in UI

```bash
# Confirm API is reachable through Caddy:
curl -k https://YOUR.SERVER.IP/YOUR_ADMIN_PATH/api/health

# Check WireSteer logs:
docker logs awg-router | tail -30
```

If the UI loads but the Interfaces page is empty — make sure you are accessing
the UI via `https://YOUR.SERVER.IP/YOUR_ADMIN_PATH/` (with trailing slash).
Without the trailing slash, relative API paths resolve incorrectly.

### Caddy certificate errors

```bash
# Re-issue the certificate:
~/.acme.sh/acme.sh --issue --server letsencrypt -d YOUR.SERVER.IP \
  --standalone --certificate-profile shortlived --days 3 --force

# Reinstall:
~/.acme.sh/acme.sh --install-cert -d YOUR.SERVER.IP \
  --key-file /etc/ssl/wiresteer/server.key \
  --fullchain-file /etc/ssl/wiresteer/server.crt \
  --reloadcmd "docker exec wiresteer-caddy caddy reload --config /etc/caddy/Caddyfile 2>/dev/null || true"

# Restart Caddy:
cd ~/awg-easy/deploy/caddy && docker compose restart
```

### WireGuard tunnel not passing traffic

```bash
# Check interface is up:
ip -d link show wg10

# Check NAT rule exists:
iptables-nft -t nat -L POSTROUTING -n -v | grep MASQUERADE

# Check IP forwarding:
sysctl net.ipv4.ip_forward    # must be 1

# If PostUp rules are missing — Stop and Start the interface in the UI
```

### QUIC UDP buffer warning in Caddy logs

```
failed to sufficiently increase receive buffer size (was: 208 kiB, wanted: 7168 kiB, got: 416 kiB)
```

This is a warning, not an error. HTTP/3 works but with a smaller buffer.
To silence it, apply the sysctl settings from Step 4 and restart Caddy.

---

## acme.sh auto-renewal

acme.sh installs a cron job automatically during installation. Verify:

```bash
crontab -l | grep acme
# expected: something like: 0 0 * * * /root/.acme.sh/acme.sh --cron --home /root/.acme.sh ...
```

On renewal, acme.sh runs the `--reloadcmd` configured in Step 9,
which reloads Caddy config without downtime.
