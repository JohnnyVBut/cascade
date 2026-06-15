# Plan: On-Demand Speed Test Between Cascade Servers

## Overview

Allow the user to run an iperf3-based speed test between any two Cascade nodes (local or remote) directly from the Remotes page in the UI. The local server orchestrates the test by calling the speedtest API endpoints on each node (via the existing `/api/remotes/:id/proxy/*` mechanism).

---

## Architecture Summary

```
Browser → Local Cascade UI
  → POST /api/remotes/A/proxy/speedtest/server   (start iperf3 -s on Server A)
  ← { port, sessionId }
  → POST /api/remotes/B/proxy/speedtest/client   body: { host: <A's IP>, port, duration }
  ← { uploadMbps, downloadMbps, retransmits, latencyMs }
  → DELETE /api/remotes/A/proxy/speedtest/server/:sessionId  (cleanup)
```

When A or B is "local", the path is `/api/speedtest/...` (no proxy prefix) — this is handled automatically by `api.js` `call()` when `_remoteId` is null. Both cases are transparent because the UI always routes through `api.call()` with `setRemote()` applied per-request.

---

## Critical Constraints / Gotchas

### 1. Proxy HTTP client timeout is 5 s — iperf3 runs 10+ s

`proxyClient` in `internal/api/remotes.go` has `Timeout: 5 * time.Second`. An iperf3 client run of even 5 s will be killed mid-test. A **separate long-lived HTTP client** must be used for the speedtest client proxy call, or better: the speedtest endpoints must be excluded from `proxyClient` and use a dedicated client with `Timeout: duration + 30s`.

The cleanest fix: create `speedtestProxyClient` in `internal/api/speedtest.go` with a 120 s timeout. The `proxyRemote` handler always uses `proxyClient` — the speedtest/client endpoint does NOT go through `proxyRemote`. Instead, the orchestration happens in the **browser** (two separate `api.call()` calls with `setRemote()` applied per call), so each remote call goes through `proxyRemote` independently. The problem is that `proxyRemote` always uses the 5 s `proxyClient`.

**Solution**: Add a check in `proxyRemote` for paths matching `/speedtest/client` and use a dedicated slow-proxy client (120 s timeout). Alternatively, expose the timeout via a query param — but this opens a DoS vector. Best: path-based client selection in `proxyRemote`.

### 2. `host` parameter injection

The `host` field in `POST /api/speedtest/client` is passed directly to iperf3 as a CLI argument. Must validate it is a plain IPv4/IPv6 address (no hostname, no shell metacharacters) using `net.ParseIP()`. Reject if nil.

### 3. Port allocation collision

Random port in 20000–30000 range. Use `net.Listen("tcp", ":0")` to get an OS-assigned free port, then release the listener before spawning iperf3. Avoids race but not perfectly atomic — acceptable.

### 4. iperf3 not installed

Must check at test start, not at registration time (binary may be added/removed). `GET /api/speedtest/check` returns `{ available: bool }`.

### 5. sessionId lifecycle

Sessions are in-memory (map[string]*exec.Cmd + cancel func). Container restart clears them. Orphaned iperf3 server processes are killed by the 60 s auto-timeout context. No DB persistence needed.

### 6. Vue 2 reactivity — adding speedtest state fields

All new data fields must be declared in the `data()` return object. Map-type state (e.g. `speedtestResult`) must use `Vue.set()` or `this.$set()` for reactivity on new keys — they cannot be assigned with `obj[key] = value`.

### 7. IP address to use for iperf3 connection

Server B needs Server A's reachable IP. Options ranked:
- **Best**: extract hostname from `remote.url` (already in the `remotes` list as URL). Parse with `new URL(remote.url).hostname`. This is what's already validated against SSRF on the backend.
- Fallback: let user override via a text field labeled "Override IP".
- When A is "local": use a text field (the local server's public IP is in Settings `publicIP` or from `GET /api/health` which returns `host`). The health endpoint is already unprotected.

The UI will show the auto-extracted IP with an optional manual override field.

### 8. Concurrent tests

Only one test at a time per browser session. Frontend disables the "Run" button while `speedtestRunning` is true.

---

## Files to Create / Modify

| File | Action | Complexity |
|------|--------|------------|
| `internal/api/speedtest.go` | CREATE | Medium |
| `internal/api/remotes.go` | MODIFY — add slow proxy client + path-based selection in `proxyRemote` | Small |
| `cmd/awg-easy/main.go` | MODIFY — register speedtest routes | Small |
| `internal/frontend/www/js/api.js` | MODIFY — add speedtest API methods | Small |
| `internal/frontend/www/js/app.js` | MODIFY — add speedtest data fields + methods | Medium |
| `internal/frontend/www/index.html` | MODIFY — add Speed Test button + modal on Remotes page | Medium |

### Files NOT to modify
- `internal/remotes/remotes.go` — no schema change needed
- `internal/db/db.go` — no migration needed (in-memory sessions)
- `internal/remoteclient/` — no change
- Any firewall/routing/tunnel packages

---

## Step-by-Step Implementation Plan

### Step 1 — Backend: `internal/api/speedtest.go` (Medium)

Create file with:

```
package api

import (
    "context"
    "net"
    "os/exec"
    "sync"
    "time"
    ...
)
```

**Types:**
```go
type speedtestSession struct {
    cancel  context.CancelFunc
    port    int
}

var (
    stMu      sync.Mutex
    stSessions = map[string]*speedtestSession{}
)
```

**Functions:**

`RegisterSpeedtest(api fiber.Router)` — registers:
- `GET  /speedtest/check`
- `POST /speedtest/server`
- `POST /speedtest/client`
- `DELETE /speedtest/server/:sessionId`

`checkSpeedtest(c *fiber.Ctx) error`
- Run `exec.LookPath("iperf3")`
- Return `{ available: bool }`

`startSpeedtestServer(c *fiber.Ctx) error`
- Find free port: `net.Listen("tcp", ":0")`, record port, close listener
- Generate `sessionId = uuid.New().String()`
- Create `context.WithTimeout(ctx, 60*time.Second)`
- Run `exec.CommandContext(ctx, "iperf3", "-s", "--one-off", "-p", strconv.Itoa(port))`
- Start (not Wait) the process
- Store `stSessions[sessionId] = &speedtestSession{cancel, port}`
- goroutine: `cmd.Wait()` then `stMu.Lock(); delete(stSessions, sessionId); stMu.Unlock()`
- Return `{ sessionId, port }`

`runSpeedtestClient(c *fiber.Ctx) error`
- Parse body: `{ host string, port int, duration int }`
- Validate: `net.ParseIP(host)` — reject nil (400)
- Validate: `port` in 1024–65535 (400)
- Validate: `duration` 1–30, default 5
- Run `exec.CommandContext` with `duration + 15s` timeout:
  `iperf3 -c <host> -p <port> -t <duration> -J`
- Parse JSON stdout: extract `end.sum_sent.bits_per_second`, `end.sum_received.bits_per_second`, `end.streams[0].udp.jitter_ms` (TCP has no jitter — use `end.streams[0].sender.retransmits`)
- Return `{ uploadMbps, downloadMbps, retransmits, durationSec }`

`stopSpeedtestServer(c *fiber.Ctx) error`
- Look up `stSessions[c.Params("sessionId")]`
- Call `session.cancel()`
- Delete from map
- Return 204

**iperf3 JSON output structure to parse** (key paths):
```
end.sum_sent.bits_per_second      → uploadMbps   (divide by 1e6)
end.sum_received.bits_per_second  → downloadMbps (divide by 1e6)
end.sum_sent.retransmits          → retransmits
end.streams[0].sender.mean_rtt    → latencyMs (microseconds → ms, divide by 1000)
```

Note: `mean_rtt` is in microseconds in iperf3 JSON output.

### Step 2 — Backend: `internal/api/remotes.go` (Small)

Add a second HTTP client for long-running proxy calls:

```go
var speedtestProxyClient = &http.Client{
    Timeout: 120 * time.Second,
    Transport: &http.Transport{
        TLSNextProto:  make(map[string]func(string, *tls.Conn) http.RoundTripper),
        DialContext:   remoteclient.SafeDialContext,
    },
}
```

In `proxyRemote`, before `proxyClient.Do(req)`, add:

```go
client := proxyClient
if strings.Contains(c.Params("*"), "speedtest/client") {
    client = speedtestProxyClient
}
resp, err := client.Do(req)
```

This uses path-based selection without adding a new route or breaking existing proxy behavior.

### Step 3 — Backend: `cmd/awg-easy/main.go` (Small)

After `api.RegisterRemotes(apiGroup)`, add:
```go
api.RegisterSpeedtest(apiGroup)
```

### Step 4 — Frontend: `internal/frontend/www/js/api.js` (Small)

Add after the `testRemote` method:

```javascript
async checkSpeedtest() {
  return this.call({ method: 'get', path: '/speedtest/check' });
}

async startSpeedtestServer() {
  return this.call({ method: 'post', path: '/speedtest/server' });
}

async runSpeedtestClient({ host, port, duration }) {
  return this.call({ method: 'post', path: '/speedtest/client', body: { host, port, duration } });
}

async stopSpeedtestServer(sessionId) {
  return this.call({ method: 'delete', path: `/speedtest/server/${sessionId}` });
}
```

Note: `this.call()` automatically prepends the proxy prefix when `_remoteId` is set. The UI sets `_remoteId` to A or B before each call.

### Step 5 — Frontend: `internal/frontend/www/js/app.js` (Medium)

**New data fields** (add to `data()` return object):
```javascript
showSpeedtest: false,
speedtestFrom: '__local__',   // '__local__' or remote id
speedtestTo: '__local__',
speedtestDuration: 5,
speedtestRunning: false,
speedtestResult: null,   // { uploadMbps, downloadMbps, retransmits, latencyMs } or null
speedtestError: null,
speedtestHostOverride: '',  // manual IP override for Server A
```

**New computed property** — `speedtestFromIp`:
```javascript
speedtestFromIp() {
  if (this.speedtestHostOverride) return this.speedtestHostOverride;
  if (this.speedtestFrom === '__local__') return ''; // must be filled by user — local IP unknown without extra call
  const r = this.remotes.find(r => r.id === this.speedtestFrom);
  if (!r) return '';
  try { return new URL(r.url).hostname; } catch { return ''; }
}
```

**New method** — `runSpeedtest()`:
```javascript
async runSpeedtest() {
  if (this.speedtestFrom === this.speedtestTo) {
    this.showToast('Source and destination must be different', 'error');
    return;
  }
  const host = this.speedtestFromIp;
  if (!host) {
    this.showToast('Cannot determine Server A IP. Use IP override field.', 'error');
    return;
  }
  this.speedtestRunning = true;
  this.speedtestResult = null;
  this.speedtestError = null;
  let sessionId = null;
  try {
    // 1. Start server on A
    this.speedtestFrom !== '__local__' ? api.setRemote(this.speedtestFrom) : api.clearRemote();
    const srv = await api.startSpeedtestServer();
    sessionId = srv.sessionId;

    // 2. Run client on B
    this.speedtestTo !== '__local__' ? api.setRemote(this.speedtestTo) : api.clearRemote();
    const result = await api.runSpeedtestClient({ host, port: srv.port, duration: this.speedtestDuration });
    this.speedtestResult = result;
  } catch (err) {
    this.speedtestError = err.message;
    this.showToast(`Speed test failed: ${err.message}`, 'error');
  } finally {
    // 3. Cleanup server on A
    if (sessionId) {
      try {
        this.speedtestFrom !== '__local__' ? api.setRemote(this.speedtestFrom) : api.clearRemote();
        await api.stopSpeedtestServer(sessionId);
      } catch {}
    }
    // Restore active remote
    this.activeRemoteId ? api.setRemote(this.activeRemoteId) : api.clearRemote();
    this.speedtestRunning = false;
  }
}
```

**Important**: after the test, restore `api._remoteId` to `this.activeRemoteId` (the currently active remote the user had selected before the test). This prevents corrupting the global API routing state.

### Step 6 — Frontend: `internal/frontend/www/index.html` (Medium)

**Location**: On the Remotes page, add a "Speed Test" button next to the existing "+ Add Server" button. The button opens `showSpeedtest = true`.

**New modal** (add after the "Add Remote Modal" block, around line 4400+):

Modal structure:
- Title: "Speed Test"
- "From" dropdown: local + all remotes
- IP/Host field (auto-filled from URL hostname, editable): "Server A reachable IP"
- "To" dropdown: local + all remotes  
- Duration selector: 5 / 10 / 30 seconds
- "Run" button (disabled while running, shows spinner)
- Results section (shown when `speedtestResult` is not null):
  - Upload: `{{ speedtestResult.uploadMbps.toFixed(1) }} Mbps`
  - Download: `{{ speedtestResult.downloadMbps.toFixed(1) }} Mbps`
  - Retransmits: `{{ speedtestResult.retransmits }}`
- Error section (shown when `speedtestError`)

**Tailwind classes to verify exist** before use (per RULE #2):
- `text-2xl`, `font-bold`, `font-medium`, `text-sm`, `rounded-lg`, `text-white` — likely present
- Run: `grep "these-classes" internal/frontend/www/css/app.css` before using any new class

---

## Data Flow Diagram

```
UI clicks "Run"
  │
  ├─ api.setRemote(fromId)   [or clearRemote if local]
  ├─ POST /speedtest/server  → proxyRemote → remote A → iperf3 -s --one-off -p PORT
  │   ← { sessionId, port }
  │
  ├─ api.setRemote(toId)     [or clearRemote if local]
  ├─ POST /speedtest/client  → proxyRemote (slowClient) → remote B → iperf3 -c A_IP -p PORT -t DUR -J
  │   ← { uploadMbps, downloadMbps, retransmits }
  │
  ├─ api.setRemote(fromId)
  ├─ DELETE /speedtest/server/:sessionId  → remote A → kill iperf3
  │
  └─ api.setRemote(activeRemoteId)   [restore state]
```

---

## Risks and Edge Cases

| Risk | Mitigation |
|------|-----------|
| iperf3 not installed on remote | `GET /speedtest/check` before showing the modal; show warning if unavailable |
| Port already in use despite free-port probe | iperf3 will fail to bind; `startSpeedtestServer` captures stderr and returns 500 with message |
| iperf3 server on A stays alive if browser tab closes mid-test | 60 s context auto-kills it |
| Firewall blocks iperf3 port between A and B | iperf3 returns non-zero exit; error propagated to UI |
| `speedtestTo === speedtestFrom` | Validated in `runSpeedtest()` before any API call |
| `host` is a hostname (not IP) e.g. `example.com` | `net.ParseIP()` returns nil for hostnames → 400. If remote URL uses a domain name (not IP), user must use the IP override field. This is a known limitation — document in the UI tooltip. |
| proxyRemote 5 s timeout kills iperf3 client call | Fixed in Step 2 via `speedtestProxyClient` with 120 s timeout |
| Concurrent speedtest calls from different browser tabs | `stSessions` is protected by `stMu` mutex; iperf3 ports are independent; no conflict |
| api._remoteId left on wrong remote after test error | `finally` block always restores `activeRemoteId` |
| Bidirectional test (`-d` flag) | Not planned — adds complexity (needs reverse path connectivity). Unidirectional is sufficient for initial release. |

---

## Test Plan

After implementation:

1. Unit test `internal/api/speedtest_test.go`:
   - `POST /speedtest/check` when iperf3 absent (`PATH=""`)
   - `POST /speedtest/client` with invalid host (hostname → 400, shell metachar → 400)
   - `POST /speedtest/client` with port out of range → 400
   - `DELETE /speedtest/server/:id` with unknown sessionId → 404

2. Integration: manual test with two local Cascade instances on different ports.

---

## Backward Compatibility

- All new endpoints are additive — no existing routes changed.
- `proxyRemote` change is transparent: only affects paths containing `speedtest/client`, which did not exist before.
- No DB schema change — no migration needed.
- Frontend change is purely additive (new modal, new data fields).
