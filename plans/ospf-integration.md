# OSPF Dynamic Routing — Integration Assessment

**Date:** 2026-04-07
**Project:** Cascade (Go rewrite, `master` branch)
**Scope:** Evaluate complexity and architecture for adding OSPF support to the Routing tab

---

## 1. Existing Codebase Baseline

### What is already in place

| Component | Status |
|---|---|
| `internal/routing/manager.go` | Static route CRUD + kernel route reading (text-only, FIX-11) |
| `internal/api/routing.go` | REST handlers for `/api/routing/*` |
| `internal/frontend/www/index.html` | Routing page with 3 tabs: Status / Static / OSPF. OSPF tab is `<div v-if="activeRoutingTab === 'ospf'" ...>Coming soon</div>` |
| `Dockerfile.go` | Alpine-based, `amneziavpn/amneziawg-go:latest` as runtime base |
| `docker-compose.go.yml` | `cap_add: [NET_ADMIN, SYS_MODULE]`, `network_mode: host` |
| `internal/gateway/monitor.go` | Goroutine-per-target polling pattern with `ticker` + `stopCh` channel |
| `internal/util/exec.go` | `Exec(cmd, timeout, log)` — bash subprocess runner, non-Linux no-op, timeout=30s default |
| DB migrations | v12 is current; new table requires v13 migration |

### Key constraints inherited from the project

- `FIX-11`: Never use `ip -j` — text parsing only. Same discipline applies to any `vtysh`/`birdc` output parsing.
- `FIX-10`: Every subprocess needs a timeout. OSPF daemon CLI must be called with `util.Exec(..., timeout, ...)`.
- `FIX-13`: Init order matters. OSPF daemon must start after `tunnel.Init()` so WireGuard interfaces exist.
- `--network host`: Container shares host network namespace — OSPF multicast packets (224.0.0.5/6) will hit the actual host network stack. This is required for OSPF to function correctly.
- `NET_ADMIN` already granted — sufficient for routing daemon operation.

---

## 2. OSPF Daemon Options

### 2.1 FRRouting (FRR)

**What it is:** The dominant open-source routing suite. Modular: `zebra` (mandatory RIB manager) + `ospfd` (OSPFv2) + `ospf6d` (OSPFv3) + others. Communicates via Unix sockets. `vtysh` is the CLI.

**Alpine availability:** FRR is present in Alpine Linux edge/testing repositories but NOT in stable (v3.x) main. The `amneziavpn/amneziawg-go:latest` base is Alpine — its exact version is not pinned in the Dockerfile, but the runtime image uses Alpine stable. This means FRR would require either:
- Adding `@edge` Alpine apk repository, OR
- Building FRR from source in Dockerfile, OR
- Using a different base image

**Container requirements:**
- `zebra` + `ospfd` processes must run concurrently with the Go binary
- `/var/run/frr/` directory for Unix sockets
- `/etc/frr/` for config files
- `NET_ADMIN` — already have it
- `NET_RAW` — needed for OSPF raw socket multicast. NOT currently in `cap_add`.
- `SYS_ADMIN` — only needed for MPLS, not required for basic OSPF

**Process management challenge:** The container currently uses `dumb-init` as PID 1 running only `entrypoint.sh` → `cascade` (Go binary). Adding FRR requires running multiple daemons (`zebra` + `ospfd`). Options:
  - Modify `entrypoint.sh` to `exec zebra &; exec ospfd &; exec cascade`
  - Use a supervisor (s6-overlay, supervisord) — adds significant image complexity
  - Cascade Go binary manages FRR lifecycle via `os/exec` (`Start()` / `Wait()`)

**Config generation:** Cascade would need to write `/etc/frr/frr.conf` and send `SIGHUP` or call `vtysh -c "clear ip ospf"` to reload.

**Status polling:** `vtysh -c "show ip ospf neighbor"`, `vtysh -c "show ip ospf route"` — text output, parseable.

**Package size:** FRR in Alpine edge is ~15MB for `frr-ospfd` + dependencies. Significant image size increase.

### 2.2 BIRD 2 (bird2)

**What it is:** Single-binary routing daemon supporting BGP, OSPF, RIP, static. Simpler architecture than FRR — one process, one config file. CLI via `birdc` (connects to Unix socket `/var/run/bird/bird.ctl`).

**Alpine availability:** `bird2` IS available in Alpine Linux stable (v3.19+) main repository. This is a major advantage over FRR.

**Container requirements:**
- Single `bird` process alongside the Go binary
- `/var/run/bird/` for the control socket
- `/etc/bird/` for config
- `NET_ADMIN` — already have it
- `NET_RAW` — needed for raw socket multicast. NOT currently in `cap_add`.

**Process management:** Simpler than FRR (one process). Same challenge applies — need to run `bird` alongside Go binary. Go binary can manage it via `os/exec`.

**Config generation:** Cascade writes `/etc/bird/bird.conf` and calls `birdc configure` to hot-reload. BIRD config syntax is its own DSL.

**Status polling:** `birdc show ospf neighbors`, `birdc show ospf routes` — text output, parseable.

**Package size:** `bird2` in Alpine is ~2MB. Minimal image size impact.

### 2.3 Quagga (legacy, not recommended)

Largely superseded by FRR. Not in active development. Not evaluated further.

### 2.4 Recommendation: BIRD 2

BIRD 2 is the correct choice for this project because:

1. Available in Alpine stable main repository — no repo hacks needed, works with existing Dockerfile pattern
2. Single binary — simpler process management, Go binary can own the lifecycle
3. Smaller image footprint (~2MB vs ~15MB for FRR)
4. `birdc` CLI output is clean and parseable
5. Hot-reload via `birdc configure` works without restart
6. NET_ADMIN already present; only NET_RAW needs to be added

---

## 3. Architecture Design

### 3.1 Full implementation (non-MVP)

```
internal/
  ospf/
    manager.go       — OspfManager: lifecycle + config generation + status polling
    config.go        — BIRD config DSL generation (Go → bird.conf text)
    parser.go        — text parsers for birdc output
    types.go         — OspfConfig, OspfStatus, OspfNeighbor, OspfRoute structs
  api/
    ospf.go          — GET/PUT /api/ospf/config, GET /api/ospf/status, GET /api/ospf/neighbors, GET /api/ospf/routes
```

**OspfManager responsibilities:**
- Start/stop `bird` process (managed via `os/exec`, Go holds the `*exec.Cmd`)
- Watch for process exit (goroutine), restart if unexpected
- Generate `/etc/bird/bird.conf` from OspfConfig stored in SQLite
- Call `birdc configure` on config change (hot-reload)
- Poll `birdc show ospf neighbors` and `birdc show ospf routes` every N seconds (same ticker pattern as `gateway/monitor.go`)
- Cache latest status in memory with mutex; API reads from cache (no subprocess per request)

**Bird.conf generation responsibilities:**
- Router ID (auto-detect from primary IP, or user-specified)
- OSPF area configuration (area 0 by default)
- Interface list: which interfaces participate in OSPF (selected from WireGuard interfaces)
- Cost per interface (metric)
- Authentication (MD5 per interface, optional)
- Redistribute static routes (from Cascade static route table) — optional toggle
- Export learned OSPF routes to kernel routing table

**Integration points:**
- After `tunnel.Init()` in `main.go` — start `bird` (FIX-13 equivalent: interfaces must exist before OSPF starts)
- After `tunnel.Start()` / `tunnel.Restart()` — `birdc configure` to re-evaluate interfaces
- After `routing.AddRoute()` — optionally trigger `birdc configure` if redistributing static routes
- Graceful shutdown: `birdc down` before `app.Shutdown()`

**SQLite storage (new migration v13):**
```sql
CREATE TABLE IF NOT EXISTS ospf_config (
    id          INTEGER PRIMARY KEY CHECK (id = 1),  -- singleton row
    enabled     INTEGER NOT NULL DEFAULT 0,
    router_id   TEXT NOT NULL DEFAULT '',
    areas       TEXT NOT NULL DEFAULT '[]',  -- JSON array of OspfArea
    interfaces  TEXT NOT NULL DEFAULT '[]',  -- JSON array of OspfInterface
    options     TEXT NOT NULL DEFAULT '{}',  -- JSON: redistributeStatic, log level, etc.
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### 3.2 MVP (read-only status + manual config file)

The minimal viable implementation that fills the "Coming soon" placeholder:

**MVP scope:**
1. Assume user installs and configures `bird` manually (or via ENV-provided config path)
2. Cascade does NOT generate bird.conf or manage bird lifecycle
3. Cascade ONLY reads `birdc` output and exposes it via API
4. UI shows: daemon status (running/stopped), neighbor table, OSPF-learned routes

**MVP API (read-only):**
```
GET /api/ospf/status     — { running: bool, routerID: string, areas: int, uptime: string }
GET /api/ospf/neighbors  — { neighbors: [...] }
GET /api/ospf/routes     — { routes: [...] }
```

**MVP file list (3 new files, 2 modified):**
- NEW: `internal/ospf/status.go` — `birdc` caller + text parsers
- NEW: `internal/api/ospf.go` — 3 read-only handlers
- MODIFIED: `cmd/awg-easy/main.go` — register ospf routes
- MODIFIED: `internal/frontend/www/index.html` — replace "Coming soon" with status table + neighbor table

---

## 4. Files That Would Be Created or Modified

### MVP implementation

| File | Action | Change |
|---|---|---|
| `internal/ospf/status.go` | CREATE | `GetDaemonStatus()`, `GetNeighbors()`, `GetOspfRoutes()` via `birdc` CLI |
| `internal/api/ospf.go` | CREATE | `RegisterOspf()` — 3 GET handlers |
| `cmd/awg-easy/main.go` | MODIFY | Add `api.RegisterOspf(apiGroup)` call |
| `internal/frontend/www/index.html` | MODIFY | Replace OSPF placeholder with status tables (~100 lines) |
| `Dockerfile.go` | MODIFY | Add `bird2` to `apk add` |
| `docker-compose.go.yml` | MODIFY | Add `NET_RAW` to `cap_add` |

**Total MVP: 2 new files, 4 modified.**

### Full implementation (lifecycle management + config UI)

| File | Action | Complexity |
|---|---|---|
| `internal/ospf/types.go` | CREATE | Struct definitions |
| `internal/ospf/manager.go` | CREATE | Bird process lifecycle, polling goroutine, mutex cache |
| `internal/ospf/config.go` | CREATE | bird.conf template generation |
| `internal/ospf/parser.go` | CREATE | birdc text output parsers |
| `internal/api/ospf.go` | CREATE | 6+ handlers (status, config CRUD, neighbors, routes) |
| `internal/db/db.go` | MODIFY | Migration v13: ospf_config table |
| `cmd/awg-easy/main.go` | MODIFY | OspfManager init after tunnel.Init() + graceful shutdown |
| `internal/frontend/www/index.html` | MODIFY | OSPF config form + status tables (~500 lines) |
| `Dockerfile.go` | MODIFY | Add `bird2` to apk |
| `docker-compose.go.yml` | MODIFY | Add `NET_RAW` cap |

**Total full: 5 new files, 5 modified.**

### Files that must NOT be modified

- `internal/routing/manager.go` — stable, no OSPF coupling needed; OSPF uses separate package
- `internal/firewall/manager.go` — OSPF routes go into kernel via bird, no iptables interaction
- `internal/tunnel/interface.go` — no changes needed for MVP; full version only adds a `birdc configure` call after start/restart (via callback in manager)

---

## 5. Complexity Estimate

### MVP (read-only status)

| Step | Description | Complexity | Hours |
|---|---|---|---|
| 1 | Add `bird2` to Dockerfile + `NET_RAW` to docker-compose | Small | 0.5h |
| 2 | `internal/ospf/status.go`: call `birdc show ospf` + parse text output | Medium | 3h |
| 3 | `internal/api/ospf.go`: 3 GET handlers + register in main.go | Small | 1h |
| 4 | Frontend: replace "Coming soon" with status/neighbors/routes tables | Medium | 3h |
| 5 | Testing (manual in container) | Small | 1.5h |
| **MVP Total** | | | **~9 hours** |

### Full implementation (lifecycle + config UI)

| Step | Description | Complexity | Hours |
|---|---|---|---|
| 1 | MVP steps 1-5 | Medium | 9h |
| 2 | `internal/ospf/types.go` + `config.go` (bird.conf generation) | Medium | 4h |
| 3 | `internal/ospf/manager.go` (process management, polling goroutine) | Large | 6h |
| 4 | DB migration v13 + config persistence | Small | 1h |
| 5 | Full API (config CRUD, reload) | Medium | 3h |
| 6 | Frontend: config form (interfaces, areas, router-id, auth) | Large | 8h |
| 7 | Integration: tunnel start/restart triggers birdc configure | Small | 1.5h |
| 8 | Graceful shutdown + process crash recovery | Medium | 2h |
| 9 | Testing | Medium | 4h |
| **Full Total** | | | **~38-40 hours** |

---

## 6. Main Challenges and Risks

### Challenge 1: NET_RAW capability (BREAKING CHANGE risk)

OSPF uses raw IP sockets to send/receive multicast (224.0.0.5 AllSPFRouters, 224.0.0.6 AllDRouters). Bird requires `CAP_NET_RAW` to open these sockets. `NET_RAW` is NOT currently in `docker-compose.go.yml`.

Adding `NET_RAW` requires updating the compose file and re-deploying. This is a one-line change but it IS a breaking change for users who run without it — `bird` will fail with `Operation not permitted` on startup.

**Mitigation:** Document this clearly. Detect the missing capability at startup and log a clear error.

### Challenge 2: Bird process lifecycle inside Docker

The current architecture runs exactly one process under dumb-init: `cascade`. BIRD needs to be a second long-running process. Options:

- **Option A — Go manages bird**: Cascade calls `exec.Command("bird", "-c", configPath, "-P", pidPath)`, monitors the returned `*os.Process`, restarts on crash. This fits the existing pattern (no supervisor needed, Go owns everything). Downside: process management code in Go must handle PID file cleanup, `bird -s` socket path, etc.
- **Option B — entrypoint.sh starts bird**: `entrypoint.sh` starts `bird &` then execs `cascade`. Simpler startup but cascade cannot restart bird on crash (bird becomes orphan under dumb-init if entrypoint exits). Not recommended.
- **Recommendation: Option A**. Follows the pattern of `tunnel.Init()` managing wg-quick processes.

### Challenge 3: bird.conf is a custom DSL

Generating valid `bird.conf` requires encoding a non-trivial subset of BIRD's configuration language. Errors in the generated config cause bird to refuse to reload (BIRD validates on `birdc configure` and rejects if syntax errors). This is the highest-risk part of the full implementation.

**Mitigation for MVP:** Skip config generation entirely. User supplies their own bird.conf. Cascade only reads status.

**Mitigation for full version:** Maintain a strictly templated config (don't allow arbitrary text fields). Test the generated config with `bird -p` (parse-only mode) before applying.

### Challenge 4: WireGuard + OSPF multicast

WireGuard interfaces are point-to-point (type `POINTOPOINT` in kernel, not broadcast). Standard OSPF uses multicast on broadcast segments. On P2P links, OSPF uses `OSPF point-to-point` mode where DR/BDR election is skipped and neighbors are detected via HELLO unicast.

**This means:** Every WireGuard interface added to OSPF MUST be configured as `interface "wg*" { type pointopoint; }` in bird.conf. A generic "add WireGuard interface to OSPF" UI must enforce this automatically.

If this is omitted, OSPF will form adjacencies but in the wrong state (DR/BDR attempted on P2P link → adjacency may never reach Full state).

### Challenge 5: OSPF-learned routes vs. static routes vs. PBR

BIRD exports learned OSPF routes directly to the kernel routing table. These routes will appear in:
- `GET /api/routing/table` (the kernel routes tab) with `proto ospf`
- The existing frontend already color-codes `proto === 'ospf'` with a yellow badge (line 3015 of `index.html`) — this already works without any code changes

However, if a PBR rule in FirewallManager marks packets into a custom table (e.g., table 100 for vpn_kz), OSPF routes are only in the main table by default. Users expecting OSPF routes to affect PBR traffic must configure BIRD to export to custom tables — this is advanced and out of scope for MVP.

**Risk:** No code conflict, but documentation gap. Must be noted in UI.

### Challenge 6: birdc text output is verbose and semi-structured

The `birdc` CLI does not support JSON output. Text format varies slightly between BIRD versions. Parsers must be defensive (tolerate version differences, handle "no routes" / "daemon not running" gracefully).

**Mitigation:** Follow the same `parseTextRoutes()` pattern used in `internal/routing/manager.go`. Return empty lists on parse failure, never crash.

### Challenge 7: Package availability uncertainty

FRR is NOT in Alpine stable. BIRD 2 is in Alpine stable. However, the exact Alpine version of `amneziavpn/amneziawg-go:latest` is unknown. If the base image uses Alpine 3.18 or older, bird2 may not be present.

**Mitigation:** Pin a specific Alpine version in the `FROM amneziavpn/amneziawg-go:latest` line, or use `apk add bird2` with `--allow-untrusted` and an explicit repository URL if needed. Alternatively, copy the bird2 binary from a known good Alpine image in the Dockerfile.

---

## 7. MVP Scope Recommendation

The pragmatic path is a two-phase delivery:

### Phase 1 — Read-only OSPF status (MVP, ~9 hours)

**What it does:**
- Assumes user installs and runs `bird2` separately OR the Dockerfile installs it (but Cascade does not start it)
- Cascade polls `birdc show ospf`, `birdc show ospf neighbors`, `birdc show ospf routes` every 5 seconds
- Three new API endpoints (read-only): daemon status, neighbors, routes
- Frontend OSPF tab shows: daemon running status badge, neighbors table (ID, state, interface, uptime), OSPF routes table (destination, via, cost, area)
- Existing kernel routes table in Status tab already highlights OSPF routes with yellow badge — no change needed there

**What it does NOT do:**
- Does not generate bird.conf
- Does not start/stop bird
- Does not provide config UI

**Value:** Immediately useful for operators who already run bird2. Fills the placeholder.

### Phase 2 — Full config management (~29 additional hours)

- Cascade generates and manages bird.conf
- Config UI in OSPF tab: router-id, area 0 config, interface selection (with WG interface dropdown), authentication, redistribute-static toggle
- Cascade starts/stops/restarts bird as a child process
- Hot-reload on config change

---

## 8. API Contract (Full Implementation)

All endpoints follow existing patterns (JSON wrap, `fiber.Map`):

```
GET    /api/ospf/status                — { running, routerID, version, uptime, areaCount }
GET    /api/ospf/neighbors             — { neighbors: [{id, address, interface, state, uptime, priority}] }
GET    /api/ospf/routes                — { routes: [{dst, via, dev, cost, area, type}] }
GET    /api/ospf/config                — { config: OspfConfig }
PUT    /api/ospf/config                — update config + generate bird.conf + birdc configure
POST   /api/ospf/start                 — start bird daemon
POST   /api/ospf/stop                  — stop bird daemon
POST   /api/ospf/reload                — birdc configure (reload config without full restart)
```

Per CLAUDE.md Rule 0: if these endpoints are added, both `docs/API.md` and `docs/API.en.md` must be updated in the same commit.

---

## 9. Summary Table

| Dimension | MVP | Full |
|---|---|---|
| New Go packages | 1 (`internal/ospf`) | 1 (larger) |
| New Go files | 2 | 5 |
| Modified files | 4 | 5 |
| Hours estimate | ~9h | ~38-40h |
| DB migration | No | Yes (v13) |
| New capabilities | `NET_RAW` | `NET_RAW` |
| Daemon | bird2 (unmanaged) | bird2 (managed by Go) |
| Config generation | No | Yes (bird.conf DSL) |
| Breaking change | `docker-compose.go.yml` cap | Same |
| Highest risk | birdc parser robustness | bird.conf generation correctness |
| Prerequisite | bird2 apk + NET_RAW cap | Same + BIRD config DSL knowledge |

---

## 10. Do Not Modify (flags)

These files are stable and should not be touched for OSPF:

- `/Users/jenya/PycharmProjects/cascade/internal/routing/manager.go` — no OSPF coupling
- `/Users/jenya/PycharmProjects/cascade/internal/firewall/manager.go` — no interaction needed
- `/Users/jenya/PycharmProjects/cascade/internal/tunnel/interface.go` — MVP: no touch; full: minimal addition only (post-start hook)
- `/Users/jenya/PycharmProjects/cascade/internal/db/db.go` — MVP: no touch; full: add v13 migration only
