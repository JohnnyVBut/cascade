# Cascade

Self-hosted WireGuard / AmneziaWG router management platform.

Built with Go + Fiber. Replaces the legacy Node.js AWG-Easy.

## Features

- **Interfaces** — create and manage multiple WireGuard / AmneziaWG tunnel interfaces
- **Peers** — client and site-to-site (S2S) interconnect peers with QR code support
- **Routing** — static routes, policy-based routing (PBR), kernel route inspection
- **NAT** — outbound MASQUERADE / SNAT rules with alias support
- **Firewall** — filter rules (ACCEPT / DROP / REJECT) + PBR via gateway
- **Aliases** — host, network, ipset, group, port and port-group alias types
- **Gateways** — live ping + HTTP monitoring, gateway groups, failover/fallback
- **AWG2 Templates** — AmneziaWG obfuscation parameter templates + generator
- **TLS** — Let's Encrypt via acme.sh (bare IP shortlived cert or domain)
- **Decoy site** — Caddy reverse proxy serves a fake streaming site on `/`; admin UI is hidden behind a secret path

## Quick deploy

```bash
git clone https://github.com/JohnnyVBut/cascade.git cascade
cd cascade
bash deploy/setup.sh
```

See [docs/DEPLOY.md](docs/DEPLOY.md) for full instructions.

## Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.23, Fiber v2 |
| Frontend | Vue 2, Tailwind CSS (embedded in binary) |
| Database | SQLite (via modernc.org/sqlite) |
| Reverse proxy | Caddy 2 (HTTP/3 + QUIC) |
| VPN | AmneziaWG / WireGuard (`awg-quick` / `wg-quick`) |

## Documentation

- [Deploy guide](docs/DEPLOY.md)
- [API reference](docs/API.en.md)
- [Features](docs/FEATURES.md)
- [Security model](docs/SECURITY.md)

## License

MIT
