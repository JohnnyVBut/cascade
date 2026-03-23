<p align="center">
  <img src="./assets/logo.svg" width="240" alt="Cascade" />
</p>

<h1 align="center">Cascade</h1>

<p align="center">
  <strong>Self-hosted WireGuard / AmneziaWG router management platform</strong>
</p>

<p align="center">
  <a href="https://github.com/JohnnyVBut/cascade/actions/workflows/docker-publish.yml">
    <img src="https://github.com/JohnnyVBut/cascade/actions/workflows/docker-publish.yml/badge.svg" alt="Build" />
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/JohnnyVBut/cascade" alt="License" />
  </a>
  <img src="https://img.shields.io/badge/Go-1.23-blue" alt="Go 1.23" />
  <img src="https://img.shields.io/badge/AmneziaWG-2.0-purple" alt="AmneziaWG 2.0" />
</p>

<p align="center">
  <a href="README.ru.md">🇷🇺 Русский</a>
</p>

---

## ✨ Features

| Module | Description |
|--------|-------------|
| 🔌 **Interfaces** | Multiple WireGuard / AmneziaWG tunnel interfaces |
| 👥 **Peers** | Client and site-to-site (S2S) interconnect peers with QR codes |
| 🌐 **Routing** | Static routes, policy-based routing (PBR), kernel route inspection |
| 🔀 **NAT** | Outbound MASQUERADE / SNAT with alias support |
| 🛡️ **Firewall** | Filter rules (ACCEPT / DROP / REJECT) + PBR via gateway |
| 📋 **Aliases** | Host, network, ipset, group, port and port-group alias types |
| 📡 **Gateways** | Live ping + HTTP monitoring, gateway groups, automatic failover |
| 🎛️ **AWG2 Templates** | AmneziaWG 2.0 obfuscation parameter templates with built-in generator |
| 🔒 **TLS** | Let's Encrypt via acme.sh (bare IP shortlived cert or domain) |
| 🎭 **Decoy site** | Caddy reverse proxy serves a fake streaming site on `/`; admin UI hidden behind a secret path |

## 🎯 Why Cascade?

- ✅ **Go binary** — single static binary, no Node.js, no npm, no dependencies
- ✅ **Multi-interface** — manage multiple WireGuard/AWG interfaces from one UI
- ✅ **Full AmneziaWG 2.0** — S3, S4, I5 parameters, H-range obfuscation, 7 CPS profiles
- ✅ **Policy-based routing** — route traffic per-source through different gateways
- ✅ **Gateway monitoring** — ICMP ping + HTTP/S probes, auto-fallback on failure
- ✅ **HTTPS by default** — Caddy + acme.sh, works with bare IPs via Let's Encrypt shortlived certs
- ✅ **Decoy protection** — admin path is hidden; visitors see a fake streaming site

## 📋 Requirements

- Ubuntu 22.04 or 24.04 (other distros: manual setup)
- Root access
- Public IP address or domain name
- Ports: `443/tcp` (HTTPS), `51820+/udp` (WireGuard)

## 🚀 Deployment Options

### Option A — Router only (advanced users)

Run just the Cascade container. The web UI listens on **localhost only** — no public exposure, no TLS.
You are responsible for network security, authentication and access control.

```bash
git clone https://github.com/JohnnyVBut/cascade.git
cd cascade
./build-go.sh
docker compose -f docker-compose.go.yml up -d
# UI available at http://127.0.0.1:8888/
```

Use this if you already have a reverse proxy, firewall, or VPN-only access in place.
Step-by-step guide: [docs/DEPLOY.md](docs/DEPLOY.md)

### Option B — Full stack (recommended)

One command sets up everything: AmneziaWG kernel module, TLS certificate, Caddy reverse proxy
with a decoy streaming site and a hidden admin path. The router is never exposed directly to the internet.

```bash
git clone https://github.com/JohnnyVBut/cascade.git
cd cascade
sudo bash deploy/setup.sh
```

| Step | What happens |
|------|-------------|
| 0 | 1 GB swap (prevents OOM during build) |
| 1 | Kernel upgrade to HWE 6.x (Ubuntu 22.04 only) — reboot, then re-run |
| 2 | AmneziaWG kernel module install |
| 3 | Docker CE install |
| 4 | sysctl: `ip_forward`, UDP buffers |
| 5 | Build Cascade Docker image |
| 6 | Collect config interactively (IP, secret path, email) |
| 7 | Start Cascade (localhost only) |
| 8 | Issue TLS certificate via acme.sh (Let's Encrypt) |
| 9 | Start Caddy (HTTPS + decoy site + hidden admin path) |

At the end you get:
```
Admin URL: https://YOUR_IP/<secret-path>/
```

Open it, create the first admin account, done.

> **Re-run safe:** `setup.sh` is idempotent — safe to run again after a reboot or update.

## ⚙️ Configuration

Configuration is collected interactively by `setup.sh` and saved to `deploy/.env`.

| Variable | Default | Description |
|----------|---------|-------------|
| `WG_HOST` | auto-detected | Public IP or domain of the server |
| `ADMIN_PATH` | random hex | Secret path for admin UI (e.g. `/a1b2c3d4.../`) |
| `CASCADE_PORT` | `8888` | Internal port for Cascade (Caddy proxies to this) |
| `BIND_ADDR` | `127.0.0.1` | Bind address for Cascade (use `127.0.0.1` behind Caddy) |
| `ACME_EMAIL` | optional | Email for Let's Encrypt notifications |

Additional settings (WireGuard defaults, DNS, etc.) are configurable in the Web UI under **Settings**.

## 🔒 Security Model

- Admin UI is served only via `https://HOST/<ADMIN_PATH>/` — plain `https://HOST/` shows a decoy site
- HTTPS with HTTP/3 (QUIC) via Caddy
- TLS certificates: shortlived (6-day) for bare IPs, standard 90-day for domains
- Session cookie: `HttpOnly`, `Secure`, `SameSite=Strict`
- bcrypt password hashing (cost 12)
- Input validation on all API endpoints

Full threat model: [docs/SECURITY.md](docs/SECURITY.md)

## 🔄 Updating

```bash
git pull origin feature/go-rewrite
./build-go.sh
docker compose -f docker-compose.go.yml up -d
```

## 📱 Compatible VPN Clients

> ⚠️ **Standard WireGuard clients do NOT work with AmneziaWG 2.0 interfaces.**
> WireGuard 1.0 interfaces work with standard clients normally.

| Platform | App |
|----------|-----|
| Android | [Amnezia VPN](https://play.google.com/store/apps/details?id=org.amnezia.vpn) · [AmneziaWG](https://play.google.com/store/apps/details?id=org.amnezia.awg) |
| iOS / macOS | [Amnezia VPN](https://apps.apple.com/app/amneziavpn/id1600529900) · [AmneziaWG](https://apps.apple.com/app/amneziawg/id6478942365) |
| Windows | [Amnezia VPN](https://github.com/amnezia-vpn/amnezia-client/releases) · [AmneziaWG](https://github.com/amnezia-vpn/amneziawg-windows-client/releases) |
| Linux | [amneziawg-tools](https://github.com/amnezia-vpn/amneziawg-tools) · [Amnezia VPN](https://github.com/amnezia-vpn/amnezia-client/releases) |

## 🛠️ Troubleshooting

**Check container status:**
```bash
docker logs awg-router
docker compose -f deploy/caddy/docker-compose.yml logs
```

**Check WireGuard interfaces:**
```bash
docker exec awg-router awg show
docker exec awg-router wg show
```

**Check firewall / NAT:**
```bash
docker exec awg-router iptables-nft -t nat -L -n -v
docker exec awg-router ip rule show
```

**Re-run setup (e.g. after reboot or cert renewal):**
```bash
sudo bash deploy/setup.sh
```

## 🔌 REST API

Cascade exposes a full REST API — everything the web UI does, your scripts can do too.

```bash
# Authenticate
curl -c cookies.txt -X POST http://127.0.0.1:8888/api/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"yourpassword"}'

# List interfaces
curl -b cookies.txt http://127.0.0.1:8888/api/tunnel-interfaces

# Create a peer
curl -b cookies.txt -X POST http://127.0.0.1:8888/api/tunnel-interfaces/wg10/peers \
  -H "Content-Type: application/json" \
  -d '{"name":"laptop"}'
```

Use it to automate peer provisioning, integrate with your own dashboards, or build custom clients.

Full reference: [docs/API.en.md](docs/API.en.md) · [docs/API.md (RU)](docs/API.md)

## 📖 Documentation

- [Deploy guide](docs/DEPLOY.md)
- [API reference (EN)](docs/API.en.md)
- [API reference (RU)](docs/API.md)
- [Security model](docs/SECURITY.md)

## 🏗️ Stack

| Layer | Technology |
|-------|------------|
| Backend | Go 1.23, Fiber v2 |
| Frontend | Vue 2, Tailwind CSS (embedded in binary) |
| Database | SQLite (`modernc.org/sqlite`, CGO-free) |
| Reverse proxy | Caddy 2 (HTTP/3 + QUIC) |
| VPN | AmneziaWG 2.0 / WireGuard 1.0 |

## 🙏 Credits

- Based on [wg-easy](https://github.com/wg-easy/wg-easy)
- [AmneziaVPN](https://github.com/amnezia-vpn) for the AmneziaWG protocol

## 📄 License

MIT — see [LICENSE](LICENSE)

---

<p align="center">Made with ❤️ for secure and private internet access</p>
