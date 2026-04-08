# Bridge Network Mode — Implementation Plan

## Context and Goals

This plan covers adding Docker bridge network mode to Cascade as the third supported
deployment topology alongside host (`docker-compose.go.yml`) and isolated/OVS
(`docker-compose.isolated.yml`).

In bridge mode the container runs inside a standard Docker bridge network namespace.
Docker publishes a fixed UDP port range from the host into the container. WireGuard
interfaces live inside the container's network namespace. The PortPool setting is used
at interface-creation time to ensure only ports within the published range are ever
assigned.

The existing `docs/deployment-modes.md` already documents the intent. This plan
defines the exact changes required to implement it.

---

## What Makes Bridge Mode Different from Host Mode

| Dimension | Host | Bridge |
|-----------|------|--------|
| `network_mode` | `host` | `bridge` (default, omit field or leave blank) |
| Port publishing | None needed — host ports are container ports | `ports: ["51820-51899:51820-51899/udp"]` |
| iptables context | Modifies host iptables directly | Modifies container iptables — Docker manages host DNAT |
| `ip route show default` | Returns host's real default route | Returns Docker's bridge gateway (`172.17.0.1`) |
| WG interface names | Visible on host (`ip link show wg10`) | Invisible on host, only inside container netns |
| `/etc/hostname` mount | Yes — so `os.Hostname()` returns real hostname | Not needed — `hostname:` YAML field sets it |
| `SYS_MODULE` capability | Required for kernel module mode | Same requirement |
| `devices: /dev/net/tun` | Required | Required |
| `src_valid_mark` sysctl | Affects host | Affects container netns only |
| `ip_forward` sysctl | Affects host | Affects container netns only |

Key behavioral consequences:

- The PostUp `ISP=$(ip -4 route show default | awk 'NR==1{print $5}')` will resolve to
  `eth0` inside the container (Docker's bridge interface), not the host's NIC. This is
  **correct** — MASQUERADE must target the container's egress interface so packets leaving
  the container are SNATted before Docker forwards them.
- WireGuard's `ListenPort` binds inside the container namespace. Docker's port publishing
  translates host UDP traffic to that same container port via DNAT. The port numbers
  **must match** on host and container sides — the compose file uses `51820-51899:51820-51899/udp`.
- Traffic stats and iptables rules survive container restarts in the same way as isolated
  mode — the container netns is re-created on each start, so there is no "already exists"
  race (unlike host mode where the wg interface survives docker stop).

---

## Design Decisions

### Port Range Storage

The UDP port range published by Docker is stored as two new variables in `deploy/.env`:
- `WG_PORT_START` — first port in the range (e.g. `51820`)
- `WG_PORT_END` — last port in the range (e.g. `51899`)

Setup.sh writes these. The compose file uses them via `${WG_PORT_START}-${WG_PORT_END}`.

These values are also written into the `portPool` setting in the database (as
`"51820-51899"`) during setup so that QuickCreate and manual interface creation both
draw only from published ports. This is the only way to keep portPool in sync with the
published range — the container cannot introspect Docker's published ports at runtime.

### PortPool Validation on Interface Create

No new backend validation is needed. The existing `ParsePortPool` + UDP bind test in
`nextListenPortFromPoolLocked` is already the correct gate: if a user picks a port
outside the pool via the manual "Create Interface" form, the bind test will fail
(that port is not published, so it binds successfully inside the container but is
unreachable from outside — a misleading success). A soft validation that warns when
the chosen `listenPort` is not in the published range is desirable but is **low priority
and out of scope for the initial implementation**.

### No docker-compose.bridge.yml Template Variable Substitution

Docker Compose supports `${VAR}` in values **only when a `.env` file or shell
environment provides them**. The compose file is NOT processed by `envsubst` — Docker
Compose reads the file and substitutes variables from its `.env` file or the shell
environment. This means `docker-compose.bridge.yml` can safely use `${WG_PORT_START}`
and `${WG_PORT_END}` and Compose will expand them from `deploy/.env` (loaded via
`env_file:` or via the `--env-file` CLI flag).

However: `ports:` values with shell variables are only expanded in some Compose
versions. The safest and most compatible approach is to **generate** the compose file
at setup time with the port range substituted in directly, saving it as
`deploy/docker-compose.bridge.yml`. This file is gitignored.

### NETWORK_MODE in .env

A `NETWORK_MODE` key is written to `deploy/.env` so that subsequent runs of setup.sh
and switch-mode.sh know which compose file to use. Allowed values: `host`, `bridge`,
`isolated`.

### No Backend Changes Required for iptables Behavior

`generateWgConfig()` already dynamically resolves `$ISP` via `ip -4 route show default`.
In bridge mode this resolves to `eth0` (Docker bridge interface). MASQUERADE targeting
`eth0` inside the container's netns is correct — the container's kernel routes traffic
out `eth0`, and Docker's NAT takes it from there on the host side. No code change is
needed.

### "Already exists" Handling

In host mode (FIX-2), WireGuard interfaces survive container restarts and trigger the
"already exists" error. In bridge mode the container netns is destroyed and re-created
on each restart, so there are **no stale WireGuard interfaces** on startup. The
existing down→up cycle fallback in `Start()` is harmless — it will never trigger in
bridge mode, but keeping it does not hurt.

### UI and API Changes

None required. The network mode is an infrastructure concern invisible to the
application layer. PortPool is already surfaced in Settings → Global Settings as
"Port Pool". The setup.sh step that writes portPool into the database is the only
connection between deployment infrastructure and application behavior.

---

## Risk Assessment

| Risk | Likelihood | Severity | Mitigation |
|------|-----------|----------|------------|
| User sets portPool wider than published range | Medium | Medium | Bind test catches it at creation time (port binds successfully but Docker doesn't forward it — interface appears up but unreachable). Document clearly. |
| User changes portPool in Settings UI without changing Docker ports section | Medium | High | Cannot be caught at runtime. Document that changing portPool in bridge mode requires container restart with updated compose ports. |
| Generated compose file lost (git status: untracked) | Low | Medium | Keep a `docker-compose.bridge.yml.example` as the template in git; generated file is `.gitignore`d. |
| setup.sh --yes mode: WG_PORT_START/WG_PORT_END must be pre-set | Low | Low | `ask()` helper already handles this with `fail "is required"` when --yes and no default. |
| Port range conflict with host services | Low | Medium | The UDP bind test in `nextListenPortFromPoolLocked` runs inside the container, not on the host. Add a host-side `nc -uz` probe in setup.sh. |
| `ip_forward=1` in container netns: does it work? | Low | High | Confirmed by isolated mode: `entrypoint.sh` sets sysctl inside the container netns. Same behavior in bridge mode. |

---

## Files to Create or Modify

| File | Change Type | Complexity |
|------|-------------|------------|
| `deploy/docker-compose.bridge.yml.example` | CREATE — template with literal placeholder comments | Small |
| `deploy/setup.sh` | MODIFY — add Step 2b (network mode selection) and bridge-specific steps | Medium |
| `deploy/.gitignore` (or root `.gitignore`) | MODIFY — add `deploy/docker-compose.bridge.yml` | Small |
| `docs/deployment-modes.md` | MODIFY — update bridge status from "not implemented" to "implemented" | Small |

Backend files (`internal/` tree): no changes.
Frontend files (`internal/frontend/www/`): no changes.
`docker-compose.go.yml`: no changes (host mode untouched).
`docker-compose.isolated.yml`: no changes (isolated mode untouched).
`entrypoint.sh`: no changes (already handles all three netns configurations).
`Dockerfile.go`: no changes.

---

## Step-by-Step Implementation Plan

### Step 1 — Create `deploy/docker-compose.bridge.yml.example` (Small, ~30 min)

This is the template committed to git. The actual generated file at setup time has
literal port numbers substituted in.

Content differences from `docker-compose.go.yml`:
- Remove `network_mode: host`
- Add `ports:` section: `- "PORTSTART-PORTEND:PORTSTART-PORTEND/udp"`
- Remove `/etc/hostname:/host_hostname:ro` volume mount (hostname set via `hostname:`)
- Add `hostname: cascade` field
- Add `WAIT_FOR_NETWORK: 0` to environment (explicit, bridges have a default route immediately)
- Add comments explaining the bridge-specific constraints

The Web UI TCP port (`PORT` env var, default 8888) is intentionally NOT published in
the compose ports section — Caddy reverse proxy handles inbound HTTPS and forwards to
the container. Caddy needs to reach the container; since Caddy typically runs on the
host (or in a separate container on the same Docker network), a shared network is
needed. Two options:

Option A (simpler): Put Cascade and Caddy on the same user-defined Docker bridge
network. Caddy connects to `cascade:8888`. No TCP port publishing needed.

Option B: Publish `PORT` as a TCP mapping too, keep Caddy proxying to `127.0.0.1:PORT`.

**Recommendation: Option A.** This is what setup.sh should create — a named Docker
network `cascade-net` that both Cascade and Caddy join. This avoids publishing the
admin port to the host at all.

The example file uses Option A with a named network:
```yaml
networks:
  cascade-net:
    driver: bridge

services:
  cascade:
    networks:
      - cascade-net
    ports:
      - "PORTSTART-PORTEND:PORTSTART-PORTEND/udp"
```

Caddy's `docker-compose.yml` is updated (or the generated file) to also join `cascade-net`.

### Step 2 — Update `deploy/setup.sh` (Medium, ~3 hours)

This is the largest change. The following sub-steps must be added:

#### 2a — Add network mode selection (new prompt, early in script)

Insert a new Step (between existing Step 2 AWG mode and Step 3 Docker) that asks the
user to choose network mode. The choice is saved as `NETWORK_MODE` in `deploy/.env`.

```
[1] Host (default) — shares host network namespace, simplest
[2] Bridge          — isolated container netns, requires port range
```

The isolated/OVS mode is already handled by a separate workflow (`attach.sh`) and
should not be added here to avoid scope creep.

The `ask()` / `--yes` pattern already in setup.sh handles re-use of saved values:
if `NETWORK_MODE` is already in `.env`, skip the question.

#### 2b — Bridge-specific: collect port range

If `NETWORK_MODE=bridge`, prompt:
```
WireGuard UDP port range [51820-51899]:
```
Parse `WG_PORT_START` and `WG_PORT_END` from the answer. Validate:
- Both must be integers in 1–65535
- `WG_PORT_START` < `WG_PORT_END`
- Range must contain at least 1 port

Default to `51820-51899` (80 ports — enough for most deployments).

Write `WG_PORT_START` and `WG_PORT_END` to `deploy/.env`.

#### 2c — Bridge-specific: generate compose file

Generate `deploy/docker-compose.bridge.yml` by substituting the port range into the
example template. Use `sed` or a heredoc — do not use `envsubst` (not always available).

Also create a shared Docker network `cascade-net` if it does not exist:
```bash
docker network inspect cascade-net &>/dev/null || docker network create cascade-net
```

#### 2d — Bridge-specific: update portPool in database

After the Cascade container is healthy, call the settings API to set portPool:
```bash
curl -s -X PUT http://127.0.0.1:${CASCADE_PORT}/api/settings \
  -H "Content-Type: application/json" \
  -H "Cookie: ..." \
  -d "{\"portPool\": \"${WG_PORT_START}-${WG_PORT_END}\"}"
```

Problem: the API requires auth. At setup time no password exists yet (first-user
creation happens in the browser). This means portPool cannot be set via the API at
deploy time before the first login.

Alternative: write portPool directly to the SQLite database using `sqlite3`:
```bash
sqlite3 ./data/awg.db \
  "INSERT INTO settings(key,value) VALUES('portPool','${WG_PORT_START}-${WG_PORT_END}')
   ON CONFLICT(key) DO UPDATE SET value=excluded.value;"
```

This is the correct approach since the db schema is well-defined and `isValidSettingValue`
ensures only valid values are stored. This mirrors what `UpdateSettings()` does.
`sqlite3` is already installed in the container image (`apk add sqlite`) and on most
Ubuntu hosts, but if not on the host: install it in setup.sh (`apt-get install -y sqlite3`).

The `./data/awg.db` file is mounted from the host as `./data:/etc/wireguard/data`,
so the host-side path is `$REPO_DIR/data/awg.db`.

**Alternative to avoid sqlite3 dependency on host:** write portPool using
`docker exec cascade sqlite3 /etc/wireguard/data/awg.db "..."`. This uses the
sqlite3 already inside the container image.

#### 2e — Modify Step 5 (Build/Pull) and Step 7 (Start) to use correct compose file

Currently these steps hardcode `docker-compose.go.yml`. They must be changed to select
the file based on `NETWORK_MODE`:
```bash
if [[ "${NETWORK_MODE:-host}" == "bridge" ]]; then
  COMPOSE_FILE="$REPO_DIR/deploy/docker-compose.bridge.yml"
else
  COMPOSE_FILE="$REPO_DIR/docker-compose.go.yml"
fi
```

The health check URL stays the same (`http://127.0.0.1:${CASCADE_PORT}/api/health`)
but in bridge mode `127.0.0.1:${CASCADE_PORT}` must be reachable from the host. Since
we chose Option A (named network, not publishing TCP), the health check must be done
from inside the Caddy proxy or via `docker exec`. For setup.sh simplicity, publish the
TCP port temporarily for the health check and then remove it — this is complex.

Simpler solution: in bridge mode, also publish the Web UI TCP port to localhost only
for health checking, `127.0.0.1:${CASCADE_PORT}:${CASCADE_PORT}/tcp`. This is safe
because BIND_ADDR=127.0.0.1 inside the container means the cascade process only listens
on loopback anyway — publishing the port just makes it accessible from the host loopback.

Update `docker-compose.bridge.yml.example` to include:
```yaml
ports:
  - "127.0.0.1:${CASCADE_PORT}:${CASCADE_PORT}/tcp"
  - "PORTSTART-PORTEND:PORTSTART-PORTEND/udp"
```

#### 2f — Modify `save_env()` to persist new variables

Add to the `save_env()` heredoc:
```bash
NETWORK_MODE=${NETWORK_MODE:-host}
WG_PORT_START=${WG_PORT_START:-}
WG_PORT_END=${WG_PORT_END:-}
```

#### 2g — Caddy integration in bridge mode

Caddy must be able to reach Cascade. In bridge mode with the named network approach,
update the Caddy compose file generation to add the `cascade-net` external network.
The `CASCADE_PORT` env var already carries the correct port — Caddy's upstream config
becomes `cascade:${CASCADE_PORT}` instead of `127.0.0.1:${CASCADE_PORT}`.

This touches `deploy/caddy/docker-compose.yml` or the generated Caddy compose. Review
whether Caddy already works as a separate Docker Compose project — it does (Step 9 of
setup.sh `cd "$CADDY_DIR" && $COMPOSE_CMD up -d`). The Caddy compose file needs:
```yaml
networks:
  cascade-net:
    external: true
```
and the `cascade-caddy` service must join `cascade-net`.

Caddy's `Caddyfile` must also change its upstream from `localhost:{$CASCADE_PORT}` to
`cascade:{$CASCADE_PORT}` in bridge mode. The simplest approach: make the upstream
address an env variable `CASCADE_UPSTREAM` written by setup.sh.

### Step 3 — Update `.gitignore` (Small, ~5 min)

Add `deploy/docker-compose.bridge.yml` to the root `.gitignore` (or create
`deploy/.gitignore`). The example template stays in git; the generated file does not.

### Step 4 — Update `docs/deployment-modes.md` (Small, ~15 min)

Change bridge status from "not implemented" to "implemented". Update the table.
Update the Setup-скрипт section with the actual flow.

---

## Implementation Order (with dependencies)

```
Step 1 (compose template)
  └─ Step 2a (mode selection in setup.sh)
       └─ Step 2b (port range collection)
            └─ Step 2c (compose file generation)
                 └─ Step 2d (portPool DB write)
            └─ Step 2e (compose file selection for pull/start)
                 └─ Step 2f (save_env updates)
                 └─ Step 2g (Caddy integration)
Step 3 (gitignore) — independent
Step 4 (docs) — independent
```

---

## Files NOT to Modify

| File | Reason |
|------|--------|
| `internal/tunnel/interface.go` | `generateWgConfig()` is already correct for bridge mode |
| `internal/tunnel/manager.go` | Port selection logic already correct via portPool |
| `internal/settings/settings.go` | `ParsePortPool` already validates ranges correctly |
| `internal/api/interfaces.go` | No API surface change needed |
| `docker-compose.go.yml` | Host mode must stay unchanged (no regression) |
| `docker-compose.isolated.yml` | Isolated/OVS mode must stay unchanged |
| `entrypoint.sh` | Already correct for all three netns configurations |
| `Dockerfile.go` | No runtime dependency changes |
| `deploy/caddy/docker-compose.yml` | Only generated/modified by setup.sh, not by hand |
| `internal/frontend/www/` | No UI changes needed |

---

## Backward Compatibility

- Existing host-mode deployments: zero impact. `setup.sh` checks `NETWORK_MODE` from
  `.env` and defaults to `host` if not set. All existing `.env` files without
  `NETWORK_MODE` continue to work unchanged.
- `switch-mode.sh` currently only switches AWG userspace vs kernel. It does not need
  to handle network mode switching — changing from host to bridge requires container
  restart with a different compose file, which is a deliberate destructive operation
  best done manually. Document this limitation.
- The health check URL (`http://127.0.0.1:${CASCADE_PORT}/api/health`) works in both
  modes if the TCP port is published to localhost (proposed in Step 2e).

---

## Edge Cases

1. **Port range with gaps**: `ParsePortPool` handles comma-separated lists and ranges.
   E.g. `"51820-51830,51850-51870"`. The compose `ports:` section requires a contiguous
   range for Docker. setup.sh must validate that the user-provided range is contiguous
   (no gaps) and reject non-contiguous inputs with a clear error message. The portPool
   stored in the database can be the full parsed set (including gaps) — the bind test
   handles gaps — but the Docker ports section must use contiguous ranges only.

2. **Expanding the port range later**: The user must update the compose file and restart
   the container. Add a note to setup.sh's summary output and docs. There is no mechanism
   to detect if currently assigned ports fall outside a narrowed range — document that
   narrowing the range without checking assigned ports can leave interfaces unreachable.

3. **Port range collision with running services**: setup.sh should probe each port in
   the range on the host before generating the compose file: `ss -ulnp | grep ':PORT '`.
   If any port is occupied, warn and ask the user to choose a different range.

4. **First run with `--yes` flag**: `WG_PORT_START` and `WG_PORT_END` must be set in
   `.env` before running `--yes` in bridge mode. Document in the summary.

5. **portPool wider than published range**: If a user manually widens portPool in the
   Settings UI to include ports beyond the Docker-published range, the bind test will
   succeed (the port is free inside the container) but traffic will be silently dropped
   by Docker (no DNAT rule). This is a user configuration error. Consider adding a
   `WG_PORT_RANGE` env var that the backend can read and use to validate portPool
   updates — but this is an enhancement beyond the initial implementation scope.

---

## Time Estimates

| Step | File(s) | Complexity | Estimated Hours |
|------|---------|------------|----------------|
| 1. `docker-compose.bridge.yml.example` | 1 new file | Small | 0.5h |
| 2a. Network mode selection in setup.sh | `deploy/setup.sh` | Small | 0.5h |
| 2b. Port range collection + validation | `deploy/setup.sh` | Small | 0.5h |
| 2c. Compose file generation (sed/heredoc) | `deploy/setup.sh` | Medium | 0.5h |
| 2d. portPool DB write via docker exec | `deploy/setup.sh` | Small | 0.5h |
| 2e. Compose file selection for pull/start/health | `deploy/setup.sh` | Small | 0.5h |
| 2f. save_env() extension | `deploy/setup.sh` | Small | 0.25h |
| 2g. Caddy compose integration (network + upstream) | `deploy/setup.sh`, Caddy compose | Medium | 1.0h |
| 3. `.gitignore` update | `.gitignore` | Small | 0.1h |
| 4. `docs/deployment-modes.md` update | docs | Small | 0.25h |
| Testing on a fresh Ubuntu 22.04 VM | — | — | 2.0h |
| **Total** | | | **~6.5h** |

---

## Summary of New Variables in `deploy/.env`

| Variable | Example | Description |
|----------|---------|-------------|
| `NETWORK_MODE` | `bridge` | Deployment mode: `host` (default) or `bridge` |
| `WG_PORT_START` | `51820` | First UDP port in the published range |
| `WG_PORT_END` | `51899` | Last UDP port in the published range |

These are written by setup.sh and read by setup.sh on subsequent runs.
They are NOT injected into the container (no application-level behavior change).

The portPool stored in SQLite (`51820-51899`) is the authoritative source for
interface creation logic — it is derived from these variables at setup time.
