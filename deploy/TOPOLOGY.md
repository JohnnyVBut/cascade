# Cascade — Deploy Topology

Operational reference. Update this file whenever infrastructure details change.

---

## Directory layout on the host

```
/root/cascade/                  ← git repo root (cloned here by setup.sh)
├── data/                       ← runtime data (bind-mounted into container)
│   ├── cascade.db              ← main SQLite database
│   ├── cascade.db-wal          ← SQLite WAL (exists only while server runs)
│   ├── cascade.db-shm          ← SQLite shared memory
│   ├── metrics.db              ← traffic metrics (separate DB, not in backups by default)
│   ├── *.save                  ← ipset snapshots restored on startup
│   └── pre-restore-*.tar.gz   ← auto-backups created before each restore
├── deploy/
│   ├── .env                    ← runtime config written by setup.sh (WG_HOST, PASSWORD_HASH, etc.)
│   └── caddy/
│       ├── .env                ← Caddy secrets: ADMIN_PATH (written by setup.sh)
│       └── docker-compose.yml
└── docker-compose.yml
```

---

## Volumes

| What | Type | Host path | Container path |
|------|------|-----------|----------------|
| Runtime data | **bind mount** | `/root/cascade/data/` | `/etc/wireguard/data/` |
| Host hostname | bind mount (ro) | `/etc/hostname` | `/host_hostname` |
| TLS certs | bind mount (ro) | `/etc/ssl/cascade/` | `/certs` |
| acme.sh webroot | bind mount (ro) | `/srv/acme/` | `/srv/acme` |
| Caddy logs | named volume | `caddy_logs` (Docker-managed) | `/logs` |

> ⚠️ `cascade_data` is NOT a named volume — it does not exist.
> All cascade runtime data lives in the bind-mounted `/root/cascade/data/`.
> Docker commands using `-v cascade_data:...` operate on an **empty throwaway volume** and have no effect on the real data.

---

## Container network

Both containers use `network_mode: host` — they share the host network namespace.

```
Internet
   │
   ├─ :80, :443   ──► cascade-caddy  (Caddy, host network)
   │                       │
   │                       │ reverse-proxy to 127.0.0.1:8888
   │                       ▼
   │                  cascade  (Go app, host network, listens on BIND_ADDR:CASCADE_PORT)
   │
   └─ :51820/udp  ──► cascade  (WireGuard/AWG, directly on host)
```

| Container | Image | Listen |
|-----------|-------|--------|
| `cascade` | `ghcr.io/johnnyvbut/cascade:latest` | `127.0.0.1:8888` (HTTP, internal only) |
| `cascade-caddy` | built from `deploy/caddy/` | `:80`, `:443` (HTTPS/HTTP3, public) |

Cascade is **not reachable directly from the internet** — only through Caddy.

---

## Admin URL

Caddy exposes Cascade under a secret random path (set by setup.sh):

```
https://<WG_HOST>/<ADMIN_PATH>/
```

To find your ADMIN_PATH:
```bash
grep ADMIN_PATH /root/cascade/deploy/caddy/.env
```

All other paths (`/`) serve the decoy static site.

---

## Config files

| File | Written by | Contains |
|------|-----------|----------|
| `deploy/.env` | `setup.sh` / `switch-mode.sh` | `WG_HOST`, `PASSWORD_HASH`, `CASCADE_PORT`, `BIND_ADDR`, `WG_PORT`, `ADMIN_PATH` (legacy), `DEBUG` |
| `deploy/caddy/.env` | `setup.sh` | `ADMIN_PATH`, `WG_HOST`, `CASCADE_PORT` |
| `data/cascade.db` | Cascade (runtime) | All app state: users, peers, interfaces, NAT, firewall, routing, gateways, settings |

---

## Operations quick reference

### Check what's running
```bash
docker ps
docker logs cascade --tail=50
docker logs cascade-caddy --tail=20
```

### Restart
```bash
docker compose restart cascade
docker compose -f deploy/caddy/docker-compose.yml restart caddy
```

### Deploy update
```bash
cd /root/cascade && git pull && ./build.sh
docker compose down && docker compose up -d
```

### Access the DB directly (e.g. during recovery)
```bash
# DO NOT use -v cascade_data — use the actual bind mount path:
docker run --rm -v /root/cascade/data:/data alpine sh -c "apk add sqlite && sqlite3 /data/cascade.db"
```

### Manual backup
```bash
# WAL checkpoint first — otherwise the .db file may be inconsistent
docker exec cascade sh -c "sqlite3 /etc/wireguard/data/cascade.db 'PRAGMA wal_checkpoint(FULL);'"
cp /root/cascade/data/cascade.db /root/cascade-backup-$(date +%Y%m%d-%H%M%S).db
```

### Restore from backup tar.gz
```bash
docker compose down
cd /root/cascade/data
tar xzf /path/to/cascade-backup-XXXX.tar.gz
rm -f cascade.db-wal cascade.db-shm
docker compose up -d
```

---

## Known gotchas

| # | Symptom | Root cause | Fix |
|---|---------|-----------|-----|
| 1 | `cascade_data` volume operations have no effect | It's a bind mount, not a named volume. Real data is at `/root/cascade/data/` | Use `/root/cascade/data/` directly on the host |
| 2 | `database disk image is malformed` on startup after restore | Stale `cascade.db-wal` from the previous process is incompatible with the restored DB file | `rm -f /root/cascade/data/cascade.db-wal /root/cascade/data/cascade.db-shm` then restart |
| 3 | Backup file opens as malformed on another server | Backup was created without WAL checkpoint — WAL pages not flushed to main file | Fixed in code: `PRAGMA wal_checkpoint(FULL)` now runs before reading the DB file |
| 4 | Cascade not accessible after `docker exec` port check | Cascade listens on `127.0.0.1:8888`, not `0.0.0.0` — only accessible through Caddy proxy | Check via `curl -sk https://localhost/ADMIN_PATH/api/health` or look in `deploy/caddy/.env` |
| 5 | `ip -j` command hangs forever | The `-j` (JSON) flag for `ip` hangs on some Linux kernels | Forbidden in codebase — use text output + manual parsing only |
