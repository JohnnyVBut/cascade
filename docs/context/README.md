> ⚠️ **OBSOLETE — Node.js era context document**
>
> This file was written on 30 January 2026 during the initial Node.js prototype phase
> (feature/kernel-module branch). It describes the original `TunnelInterface.js` /
> `InterfaceManager.js` / `Peer.js` architecture that was later fully rewritten in Go.
>
> **Do not use this as a reference.** Current architecture is documented in:
> - [docs/ARCHITECTURE.md](../ARCHITECTURE.md) — Go rewrite architecture overview
> - [docs/API.en.md](../API.en.md) — current REST API reference (English)
> - [docs/API.md](../API.md) — current REST API reference (Russian)
> - [docs/FEATURES.md](../FEATURES.md) — current feature list
>
> The Go rewrite stores all data in SQLite (`/etc/wireguard/data/awg.db`).
> JSON files under `/etc/wireguard/data/interfaces/` and `/etc/wireguard/data/peers/`
> are no longer used.
