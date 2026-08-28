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
  <img src="https://img.shields.io/badge/AmneziaWG-3.0-purple" alt="AmneziaWG 3.0" />
</p>

<p align="center">
  <a href="README.ru.md">🇷🇺 Русский</a>
  &nbsp;·&nbsp;
  <a href="docs/USER_MANUAL.en.md">📖 User Manual</a>
</p>

---

<img width="1484" height="775" alt="image" src="https://github.com/user-attachments/assets/01be9f90-afc5-452c-ad5e-25bfa586ba2b" />

## Contents

- [Features](#-features)
- [Requirements](#-requirements)
- [I'm new here — install Cascade](#-im-new-here--install-cascade)
- [I already have Cascade — update it](#-i-already-have-cascade--update-it)
- [Switch AWG run mode on a running install](#-switch-awg-run-mode-on-a-running-install)
- [TLS: staging vs. production certificates](#-tls-staging-vs-production-certificates)
- [Configuration reference](#️-configuration-reference)
- [Security model](#-security-model)
- [Compatible VPN clients](#-compatible-vpn-clients)
- [Troubleshooting](#️-troubleshooting)
- [REST API](#-rest-api)
- [Documentation](#-documentation)
- [Stack](#️-stack)
- [Support the project](#-support-the-project)

---

## ✨ Features

| Module | Description |
|--------|-------------|
| 🔌 **Interfaces** | Multiple WireGuard / AmneziaWG tunnel interfaces (2.0 and **3.0**), quick-create in one click, import `.conf` as uplink, per-interface MSS clamping |
| 👥 **Peers** | Client and site-to-site (S2S) interconnect peers with QR codes, lifetime traffic stats, per-client bandwidth limiting and group membership |
| 🌐 **Routing** | Static routes, policy-based routing (PBR), kernel route inspection, OSPF is on plans |
| 🔀 **NAT** | Outbound MASQUERADE / SNAT with alias support + Port Forwarding (DNAT) with per-interface scoping |
| 🛡️ **Firewall** | Filter rules (ACCEPT / DROP / REJECT) + PBR via gateway |
| 📋 **Aliases** | 7 types: host, network, ipset, client-group, group, port, port-group. Client groups are ipset-backed and auto-updated on peer changes |
| 📡 **Gateways** | Live ping + HTTP monitoring, gateway groups, automatic failover |
| 🎛️ **AWG Templates** | AmneziaWG 2.0 and **3.0** obfuscation parameter templates with a built-in generator, including AWG 3.0 Transport Protection (S3/S4) fields |
| 🔐 **Auth** | Multi-user accounts, TOTP 2FA (Google Authenticator), long-lived API tokens |
| 🔒 **TLS** | Let's Encrypt via acme.sh (bare IP shortlived cert or domain) |
| 🎭 **Decoy site** | Caddy reverse proxy serves a fake streaming site on `/`; admin UI hidden behind a secret path |
| 🖥️ **Multi-Server** | Manage multiple Cascade routers from one UI — switch servers in the sidebar, proxy all API calls transparently, self-signed cert support |
| 📊 **Monitoring** | Real-time traffic metrics per interface, gateway status history (stacked bar chart), Diagnostics page with per-period history |
| ⚡ **Speed Test** | iperf3-based speed test between any two managed servers — Auto / Tunnel / Internet mode, S2S tunnel autodetect, result history |
| 🚦 **Rate Limits** | Per-client-group bandwidth limiting via tc HTB (kbps down/up enforced per IP) |
| 🧙 **Wizards** | Step-by-step setup wizards: Simple Client VPN, Cascade via WireGuard Uplink, Cascade ↔ Cascade S2S interconnect |
| 💾 **Backup** | `deploy/backup.sh` — one-command data backup before upgrading |

### Why Cascade?

- ✅ **Go binary** — single static binary, no Node.js, no npm, no dependencies
- ✅ **Multi-interface** — manage multiple WireGuard/AWG interfaces from one UI
- ✅ **Full AmneziaWG 2.0 & 3.0** — S3, S4, I5, Transport Protection, H-range obfuscation, 7 CPS profiles + browser fingerprint
- ✅ **Policy-based routing** — route traffic per-source through different gateways
- ✅ **Port Forwarding (DNAT)** — transparent traffic cascading with optional source NAT
- ✅ **Import .conf as uplink** — connect Cascade as a client to any WireGuard server; use as PBR gateway without touching the routing table
- ✅ **Gateway monitoring** — ICMP ping + HTTP/S probes, auto-fallback on failure
- ✅ **Multi-user + TOTP 2FA** — per-user accounts with Google Authenticator support
- ✅ **HTTPS by default** — Caddy + acme.sh, works with bare IPs via Let's Encrypt shortlived certs
- ✅ **Decoy protection** — admin path is hidden; visitors see a fake streaming site
- ✅ **Multi-server management** — control multiple Cascade routers from one browser tab, with transparent API proxying
- ✅ **Built-in speed test** — iperf3 between any managed servers, S2S tunnel autodetect, result history
- ✅ **Traffic monitoring** — per-interface metrics and gateway health history with configurable time periods
- ✅ **Setup wizards** — guided wizards for Uplink VPN and S2S interconnect; auto-create interfaces, aliases, gateways, PBR rules and NAT in one flow

---

## 📋 Requirements

- Ubuntu 22.04 or 24.04 (other distros: manual setup)
- Root access
- Public IP address or domain name
- Ports: `443/tcp` (HTTPS), `51820+/udp` (WireGuard)
- `git` (minimal/stripped-down VPS images often don't ship it — install first if `git clone` below fails with "command not found"):
  ```bash
  apt-get update && apt-get install -y git
  ```

---

## 🆕 I'm new here — install Cascade

Two decisions to make before you run anything:

1. **Deployment option** — almost everyone wants **Full stack** (HTTPS, decoy site, everything wired up).
   Pick **Router only** only if you already run your own reverse proxy / firewall / VPN-only access.
2. **AWG run mode** — **Userspace** is the recommended default: works on any VPS, no reboot, no kernel deadlocks.
   Pick **Kernel module** only if you need maximum throughput and can tolerate its
   [known deadlock issues](https://github.com/amnezia-vpn/amneziawg-linux-kernel-module/issues/146).

### The fastest path (Full stack + Userspace)

One command, on a fresh VPS, as root:

```bash
curl -fsSL https://raw.githubusercontent.com/JohnnyVBut/cascade/master/deploy/quickstart-userspace.sh | sudo bash
```

That's it. At the end you'll get an admin URL like `https://YOUR_IP/<secret-path>/` — open it,
create your first user account (no auth required until the first account exists), and enable
**TOTP 2FA** in Settings → Users.

> If `curl` hangs or times out talking to `raw.githubusercontent.com`, some networks/providers
> can't route to GitHub's Fastly-backed raw-content CDN (`185.199.108-111.133`) even though
> `github.com` itself is reachable. Clone instead and run the script locally:
> ```bash
> apt-get update && apt-get install -y git
> git clone https://github.com/JohnnyVBut/cascade.git
> cd cascade
> sudo bash deploy/quickstart-userspace.sh
> ```

Want kernel module mode instead? Same idea, different script:

```bash
curl -fsSL https://raw.githubusercontent.com/JohnnyVBut/cascade/master/deploy/quickstart-kernel.sh | sudo bash
```

Testing and don't want to burn Let's Encrypt's production rate limit? Add `--staging` to either
script — see [TLS: staging vs. production](#-tls-staging-vs-production-certificates).

<details>
<summary><strong>Prefer to run the steps yourself, or need Router-only / Bridge network mode?</strong></summary>

#### Full stack, step by step

```bash
git clone https://github.com/JohnnyVBut/cascade.git
cd cascade
sudo bash deploy/setup.sh          # interactive — asks run mode, network mode, IP, admin path, email
# or: sudo bash deploy/setup.sh --yes   (all defaults: userspace, host network, auto-detected IP)
```

| Step | What happens |
|------|-------------|
| 0 | 1 GB swap (prevents OOM during build) |
| 1 | Kernel upgrade to HWE 6.x (Ubuntu 22.04 only) — reboot, then re-run |
| 2 | **AmneziaWG run mode** — choose Userspace (recommended) or Kernel module |
| 2b | **Docker network mode** — choose Host (default) or Bridge (port range for Docker publish) |
| 3 | Docker CE install |
| 4 | sysctl: `ip_forward`, UDP buffers |
| 4b | TCP tuning: BBR congestion control, FQ scheduler, `rp_filter` |
| 5a | Generate decoy video via ffmpeg (60 s noise — looks like a real stream) |
| 5 | Build Cascade Docker image |
| 6 | Collect config interactively (IP, secret path, email) |
| 7 | Start Cascade (localhost only) |
| 8 | Issue TLS certificate via acme.sh (Let's Encrypt) |
| 9 | Start Caddy (HTTPS + decoy site + hidden admin path) |
| 10 | Verify: health-check Cascade + Caddy, print summary |

`setup.sh` is idempotent — safe to re-run after a reboot or an update.

#### Router only (advanced)

Just the Cascade container, listening on **localhost only** — no public exposure, no TLS.
You're responsible for network security, authentication and access control yourself.

```bash
git clone https://github.com/JohnnyVBut/cascade.git
cd cascade
docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml up -d
# UI available at http://127.0.0.1:8888/
```

Step-by-step guide: [docs/DEPLOY.md](docs/DEPLOY.md)

</details>

---

## 🔄 I already have Cascade — update it

Which command to run depends only on **AWG run mode** — check the badge in the web UI sidebar
(blue = userspace, green = kernel) if you're not sure. See [AWG Run Modes](#-awg-run-modes) for
what each mode means.

### Userspace mode

Safe, no special ordering needed — CLI and protocol implementation live in the same image and
update together atomically:

```bash
cd cascade
git pull origin master
docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml up -d
```

Full stack (Caddy) install:
```bash
cd cascade
git pull origin master
sudo bash deploy/setup.sh --yes
```

### Kernel module mode

⚠️ **Order matters here.** The Docker image tracks AmneziaWG's `:latest` protocol line, but your
host's kernel module does not update itself — if the two drift apart, interfaces fail to start
with `Unable to modify interface: Invalid argument`. This mainly bites installs from **before the
AmneziaWG 3.0 protocol jump (2026-07-30)**.

Pull the new image but **don't** run `up -d` yet — let `switch-mode.sh --kernel` resync the
kernel module *and* restart the container together, so the CLI and module flip in the same step:

```bash
cd cascade
git pull origin master
docker compose -f docker-compose.yml pull
sudo bash deploy/switch-mode.sh --kernel
```

As of v0.9.5, `--kernel` re-checks the `ppa:amnezia/ppa` package version every time — even if a
module is already loaded — and reloads it if a newer build is available, so this is a real
re-sync, not a no-op.

---

## 🔁 Switch AWG run mode on a running install

```bash
sudo bash deploy/switch-mode.sh --userspace   # → amneziawg-go (stable)
sudo bash deploy/switch-mode.sh --kernel      # → kernel module (fast)
```

The script handles kernel module install/unload, blacklisting, and container restart automatically.

> **Not the same as the quickstart scripts.** `quickstart-kernel.sh` / `quickstart-userspace.sh`
> are for a *fresh* install only — running one again on an already-set-up system does **not**
> switch modes, because `setup.sh` sources the existing `deploy/.env` and that overrides whatever
> mode the quickstart script tried to set. Use `switch-mode.sh` here instead.

---

## 🔒 TLS: staging vs. production certificates

Add `--staging` to `setup.sh` or either quickstart script to issue an untrusted certificate from
the [Let's Encrypt staging CA](https://letsencrypt.org/docs/staging-environment/) instead of a
real one:

```bash
sudo bash deploy/setup.sh --staging        # staging CA (browser shows warning — expected)
sudo bash deploy/setup.sh --yes --staging  # non-interactive + staging
```

**When to use staging:**
- Repeated installs/reinstalls on the same domain while testing — Let's Encrypt's
  [production rate limits](https://letsencrypt.org/docs/rate-limits/) (5 certs per exact domain
  set per week) are easy to hit while iterating, and staging has effectively none
- CI, throwaway VPS, or scripted install testing (e.g. the quickstart scripts)
- Any dry run where you just want to confirm the install completes and TLS wiring works, without
  needing a browser-trusted cert yet

**Switching staging → production:** remove the staging flag and re-run. `setup.sh` detects the
existing cert's issuer and swaps it automatically — no manual cert deletion needed:

```bash
sed -i '/^ACME_STAGING=/d' deploy/.env    # or: manually delete the line
sudo bash deploy/setup.sh --yes
```

`setup.sh` sees `CERT_MODE=staging` with `ACME_STAGING=0`, deletes the staging cert, and issues a
real one from the production CA. (The reverse — re-running with `--staging` when a production
cert is already installed — does *not* overwrite it, so you never accidentally throw away a
working production cert.)

---

## ⚙️ AWG Run Modes

| | Userspace (`amneziawg-go`) | Kernel module |
|---|---|---|
| Performance | ~70% of kernel | Maximum |
| Stability | ✅ Stable | ⚠️ Known deadlocks |
| Kernel module required | ❌ No | ✅ Yes |
| Works on any VPS | ✅ Yes | Depends on kernel |
| Reboot after install | ❌ No | Sometimes |

The current mode is shown as a badge in the sidebar of the web UI (blue = userspace, green = kernel).
The Docker network mode is shown as a separate badge (gray = HOST, amber = BRIDGE, red = NONE).

---

## ⚙️ Configuration reference

Configuration is collected interactively by `setup.sh` and saved to `deploy/.env`.

| Variable | Default | Description |
|----------|---------|-------------|
| `WG_HOST` | auto-detected | Public IP or domain of the server |
| `ADMIN_PATH` | random hex | Secret path for admin UI (e.g. `/a1b2c3d4.../`) |
| `PORT` | `8888` | Internal port for Cascade (Caddy proxies to this) |
| `BIND_ADDR` | `127.0.0.1` | Bind address for Cascade (use `127.0.0.1` behind Caddy) |
| `ACME_EMAIL` | optional | Email for Let's Encrypt notifications |
| `ACME_STAGING` | `0` | `1` = use LE staging CA (untrusted cert, no rate limits — for testing) |
| `AWG_USERSPACE_IMPL` | `amneziawg-go` | `amneziawg-go` or `kernel` |
| `NETWORK_MODE` | `host` | `host` or `bridge` — Docker network mode |
| `BRIDGE_PORT_RANGE` | *(bridge only)* | Published UDP port range for WireGuard in bridge mode (e.g. `51831-65535`) |

Additional settings (WireGuard defaults, DNS, etc.) are configurable in the Web UI under **Settings**.

---

## 🔒 Security Model

- Admin UI is served only via `https://HOST/<ADMIN_PATH>/` — plain `https://HOST/` shows a decoy site
- HTTPS with HTTP/3 (QUIC) via Caddy
- TLS certificates: shortlived (6-day) for bare IPs, standard 90-day for domains
- Session cookie: `HttpOnly`, `Secure`, `SameSite=Strict`
- bcrypt password hashing (cost 12)
- **Multi-user accounts** — each user has a separate username and password
- **TOTP 2FA** — Google Authenticator / Authy (enable per-user in Settings → Users)
- **API tokens** — long-lived bearer tokens for scripts; bypass TOTP; revocable
- Input validation on all API endpoints

Full threat model: [docs/SECURITY.md](docs/SECURITY.md)

---

## 📱 Compatible VPN Clients

> ⚠️ **Standard WireGuard clients do NOT work with AmneziaWG interfaces.**
> WireGuard 1.0 interfaces work with standard clients normally.

| Platform | App |
|----------|-----|
| Android | [Amnezia VPN](https://play.google.com/store/apps/details?id=org.amnezia.vpn) · [AmneziaWG](https://play.google.com/store/apps/details?id=org.amnezia.awg) |
| iOS / macOS | [Amnezia VPN](https://apps.apple.com/app/amneziavpn/id1600529900) · [AmneziaWG](https://apps.apple.com/app/amneziawg/id6478942365) |
| Windows | [Amnezia VPN](https://github.com/amnezia-vpn/amnezia-client/releases) · [AmneziaWG](https://github.com/amnezia-vpn/amneziawg-windows-client/releases) |
| Linux | [amneziawg-tools](https://github.com/amnezia-vpn/amneziawg-tools) · [Amnezia VPN](https://github.com/amnezia-vpn/amnezia-client/releases) |

---

## 🛠️ Troubleshooting

**Check container status:**
```bash
docker logs cascade
docker compose -f deploy/caddy/docker-compose.yml logs
```

**Check WireGuard interfaces:**
```bash
docker exec cascade awg show
docker exec cascade wg show
```

**Check AWG run mode:**
```bash
docker exec cascade env | grep WG_QUICK
# WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go  → userspace
# (empty or not present)                          → kernel module
```

**Check firewall / NAT:**
```bash
docker exec cascade iptables-nft -t nat -L -n -v
docker exec cascade ip rule show
```

**Switch AWG mode:**
```bash
sudo bash deploy/switch-mode.sh --userspace
sudo bash deploy/switch-mode.sh --kernel
```

**Re-run setup (e.g. after reboot or cert renewal):**
```bash
sudo bash deploy/setup.sh
```

**Back up before anything risky:**
```bash
sudo bash deploy/backup.sh
```

---

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

---

## 📖 Documentation

- [Deploy guide](docs/DEPLOY.md)
- [API reference (EN)](docs/API.en.md)
- [API reference (RU)](docs/API.md)
- [Security model](docs/SECURITY.md)

---

## 🏗️ Stack

| Layer | Technology |
|-------|------------|
| Backend | Go 1.23, Fiber v2 |
| Frontend | Vue 2, Tailwind CSS (embedded in binary) |
| Database | SQLite (`modernc.org/sqlite`, CGO-free) |
| Reverse proxy | Caddy 2 (HTTP/3 + QUIC) |
| VPN | AmneziaWG 2.0 / 3.0, WireGuard 1.0 |

---

## ☕ Support the Project

If Cascade is useful to you, consider supporting its development:

| Method | Address |
|--------|---------|
| TRC20  | `TDm1VvwoLaRdjpp7149QNacBzQtXnGresW` |
| Yoomoney RU | https://yoomoney.ru/to/4100119568549598 |

---

## 🙏 Credits

- Based on [wg-easy](https://github.com/wg-easy/wg-easy)
- [AmneziaVPN](https://github.com/amnezia-vpn) for the AmneziaWG protocol
- [Vadim-Khristenko/AmneziaWG-Architect](https://github.com/Vadim-Khristenko/AmneziaWG-Architect) — math and code for AWG 2.0 obfuscation profile generation (CPS signatures, H-ranges, browser fingerprint packet sizing)

## 📄 License

MIT — see [LICENSE](LICENSE)

---

<p align="center">Made with ❤️ for secure and private internet access</p>
