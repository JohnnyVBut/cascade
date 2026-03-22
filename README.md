# AWG-Easy 2.0

[![Build & Publish Docker Image](https://github.com/JohnnyVBut/awg-easy/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/JohnnyVBut/awg-easy/actions/workflows/docker-publish.yml)
[![Docker Pulls](https://img.shields.io/badge/docker-ghcr.io-blue)](https://github.com/JohnnyVBut/awg-easy/pkgs/container/awg-easy)
[![License](https://img.shields.io/github/license/JohnnyVBut/awg-easy)](LICENSE)

**The easiest way to run AmneziaWG 2.0 + Web-based Admin UI.**

Full support for **AmneziaWG 2.0** with proper obfuscation parameters, DNS protocol imitation, and enhanced DPI circumvention.

<p align="center">
  <img src="./assets/screenshot.png" width="802" />
</p>

## ✨ Features

* 🔐 **Full AmneziaWG 2.0 Support** - S3, S4, I5 parameters, H ranges
* 🌐 **All-in-one** - AmneziaWG + Web UI in a single container
* 📱 **Easy Setup** - One command to get started
* 👥 **Client Management** - List, create, edit, delete, enable & disable clients
* 📊 **QR Codes** - Instant client configuration via QR code
* 📥 **Download Configs** - Get client configuration files
* 📈 **Statistics** - Real-time connection stats and Tx/Rx charts
* 🎨 **Modern UI** - Automatic Light/Dark Mode, Gravatar support
* 🌍 **Multilanguage** - Support for multiple languages
* 🔗 **One-Time Links** - Temporary download links (optional)
* ⏱️ **Client Expiry** - Set expiration dates for clients (optional)
* 📊 **Prometheus Metrics** - Export metrics for monitoring
* 🍎 **macOS Compatible** - Fixed routing issues with /32 netmask

## 🎯 What Makes This Special?

Unlike other WireGuard/AmneziaWG solutions:

- ✅ **Real AmneziaWG 2.0** - Not AWG 1.x! Includes S3, S4, I5 parameters
- ✅ **Proper H Ranges** - Header obfuscation with ranges (not single values)
- ✅ **DNS Obfuscation** - Pre-configured I1 parameter for traffic masking
- ✅ **Production Values** - Battle-tested obfuscation parameters
- ✅ **macOS Fixed** - Client netmask /32 for proper routing
- ✅ **Password Fix** - Corrected bcrypt hash parsing

## 📋 Requirements

* A host with Docker installed
* Public IP address or dynamic DNS hostname

## 🚀 Quick Start

### 1. Install Docker

If you haven't installed Docker yet:

```bash
curl -sSL https://get.docker.com | sh
sudo usermod -aG docker $(whoami)
exit
```

Log in again after installation.

### 2. Generate Password Hash

```bash
docker run --rm ghcr.io/johnnyvbut/awg-easy:latest wgpw 'your-secure-password'
```

Copy the hash (the part after `PASSWORD_HASH='` without quotes).

### 3. Run AWG-Easy

Replace `YOUR_SERVER_IP` and `YOUR_PASSWORD_HASH`:

```bash
docker run -d \
  --name=awg-easy \
  --restart unless-stopped \
  \
  -e WG_HOST=YOUR_SERVER_IP \
  -e PASSWORD_HASH='YOUR_PASSWORD_HASH' \
  -e PORT=51821 \
  -e WG_PORT=51820 \
  -e WG_DEFAULT_DNS=1.1.1.1,8.8.8.8 \
  \
  -v ~/.awg-easy:/etc/amnezia/amneziawg \
  \
  -p 51820:51820/udp \
  -p 51821:51821/tcp \
  \
  --cap-add=NET_ADMIN \
  --cap-add=SYS_MODULE \
  \
  --sysctl="net.ipv4.ip_forward=1" \
  --sysctl="net.ipv4.conf.all.src_valid_mark=1" \
  \
  --device=/dev/net/tun:/dev/net/tun \
  \
  ghcr.io/johnnyvbut/awg-easy:latest
```

### 4. Access Web UI

Open in your browser:
```
http://YOUR_SERVER_IP:51821
```

Login with the password you set in step 2.

> 💡 Your configuration will be saved in `~/.awg-easy`

## 🔧 Configuration Options

### Environment Variables

| Variable | Default | Example | Description |
|----------|---------|---------|-------------|
| `WG_HOST` | - | `vpn.example.com` | **Required**. Public hostname or IP of your VPN server |
| `PASSWORD_HASH` | - | `$2y$12$...` | **Required**. Bcrypt hash for Web UI login |
| `PORT` | `51821` | `8080` | TCP port for Web UI |
| `WG_PORT` | `51820` | `12345` | UDP port for WireGuard/AmneziaWG |
| `WG_DEFAULT_DNS` | `1.1.1.1,8.8.8.8` | `8.8.8.8` | DNS servers for clients |
| `WG_DEFAULT_ADDRESS` | `10.8.0.x` | `10.6.0.x` | Client IP address range |
| `WG_MTU` | `1420` | `1380` | MTU for clients |
| `WG_PERSISTENT_KEEPALIVE` | `25` | `0` | Keepalive interval (0 to disable) |
| `WG_ALLOWED_IPS` | `0.0.0.0/0,::/0` | `192.168.1.0/24` | Allowed IPs for routing |
| `LANG` | `en` | `ru` | Web UI language |

### AmneziaWG 2.0 Parameters

**Pre-configured with production values** (customizable via environment variables):

| Variable | Default | Description |
|----------|---------|-------------|
| `JC` | `6` | Junk packet count |
| `JMIN` | `10` | Junk packet minimum size |
| `JMAX` | `50` | Junk packet maximum size |
| `S1` | `64` | Init packet junk size |
| `S2` | `67` | Response packet junk size |
| `S3` | `17` | Cookie reply junk size (AWG 2.0) |
| `S4` | `4` | Transport message junk size (AWG 2.0) |
| `H1` | `221138202-537563446` | Init packet magic header range |
| `H2` | `1824677785-1918284606` | Response packet magic header range |
| `H3` | `2058490965-2098228430` | Underload packet magic header range |
| `H4` | `2114920036-2134209753` | Transport packet magic header range |
| `I1` | DNS packet | DNS protocol imitation (tickets.widget.kinopoisk.ru) |
| `I2-I5` | Empty | Additional imitation parameters |

> 💡 **Default values are production-tested and provide strong obfuscation!**

### Optional Features

| Variable | Default | Description |
|----------|---------|-------------|
| `UI_TRAFFIC_STATS` | `false` | Enable detailed RX/TX stats |
| `UI_CHART_TYPE` | `0` | Chart type: 0=disabled, 1=line, 2=area, 3=bar |
| `WG_ENABLE_ONE_TIME_LINKS` | `false` | Enable temporary download links |
| `WG_ENABLE_EXPIRES_TIME` | `false` | Enable client expiration |
| `ENABLE_PROMETHEUS_METRICS` | `false` | Enable Prometheus metrics at `/metrics` |
| `MAX_AGE` | `0` | Session max age in minutes (0=until browser close) |

## 🐳 Using Docker Compose

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  awg-easy:
    image: ghcr.io/johnnyvbut/awg-easy:latest
    container_name: awg-easy
    restart: unless-stopped
    
    environment:
      - WG_HOST=YOUR_SERVER_IP
      - PASSWORD_HASH=YOUR_PASSWORD_HASH
      - PORT=51821
      - WG_PORT=51820
      - WG_DEFAULT_DNS=1.1.1.1,8.8.8.8
      
    volumes:
      - ./data:/etc/amnezia/amneziawg
      
    ports:
      - "51820:51820/udp"
      - "51821:51821/tcp"
      
    cap_add:
      - NET_ADMIN
      - SYS_MODULE
      
    sysctls:
      - net.ipv4.ip_forward=1
      - net.ipv4.conf.all.src_valid_mark=1
      
    devices:
      - /dev/net/tun:/dev/net/tun
```

Then run:
```bash
docker-compose up -d
```

## 📱 Client Applications

AmneziaWG 2.0 requires compatible clients:

### Android
- [Amnezia VPN](https://play.google.com/store/apps/details?id=org.amnezia.vpn) - Official client
- [AmneziaWG](https://play.google.com/store/apps/details?id=org.amnezia.awg) - Official AWG client

### iOS / macOS
- [Amnezia VPN](https://apps.apple.com/app/amneziavpn/id1600529900) - Official client
- [AmneziaWG](https://apps.apple.com/app/amneziawg/id6478942365) - Official AWG client

### Windows
- [Amnezia VPN](https://github.com/amnezia-vpn/amnezia-client/releases) - Official client
- [AmneziaWG](https://github.com/amnezia-vpn/amneziawg-windows-client/releases) - Official AWG client

### Linux
- [Amnezia VPN](https://github.com/amnezia-vpn/amnezia-client/releases) - Official client
- [amneziawg-tools](https://github.com/amnezia-vpn/amneziawg-tools) - Command-line tools

> ⚠️ **Regular WireGuard clients will NOT work with AmneziaWG 2.0!**

## 🔄 Updating

To update to the latest version:

```bash
docker stop awg-easy
docker rm awg-easy
docker pull ghcr.io/johnnyvbut/awg-easy:latest
```

Then run the `docker run` command again.

Or with docker-compose:
```bash
docker-compose pull
docker-compose up -d
```

## 🛠️ Troubleshooting

### Connection Issues

Run diagnostics:
```bash
docker exec awg-easy wg show
docker exec awg-easy iptables -t nat -L -n -v
```

### Password Not Working

Generate a new hash:
```bash
docker run --rm ghcr.io/johnnyvbut/awg-easy:latest wgpw 'new-password'
```

### macOS Client Can't Route Traffic

Make sure client config uses `/32` netmask:
```ini
[Interface]
Address = 10.8.0.2/32  # Not /24!
```

### Logs

View container logs:
```bash
docker logs -f awg-easy
```

## 📖 Documentation

- [FINAL_SUMMARY.md](FINAL_SUMMARY.md) - Complete feature list and changelog
- [MACOS_FIX.md](MACOS_FIX.md) - macOS routing fix details
- [PASSWORD_FIX.md](PASSWORD_FIX.md) - Password authentication fix
- [REAL_CONFIG_ANALYSIS.md](REAL_CONFIG_ANALYSIS.md) - AWG 2.0 parameters explanation

## 🙏 Credits

- Based on [wg-easy](https://github.com/wg-easy/wg-easy) by wg-easy community
- AmneziaWG integration inspired by [amnezia-wg-easy](https://github.com/spcfox/amnezia-wg-easy)
- [AmneziaVPN](https://github.com/amnezia-vpn) for AmneziaWG protocol

## 📄 License

This project is licensed under the terms of the license included in this repository.

## ⭐ Support

If this project helps you, please consider giving it a star on GitHub! ⭐

---

**Made with ❤️ for secure and private internet access**
