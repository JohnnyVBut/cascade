# Plan: Persistent Traffic Stats (RX/TX) Across Container Restarts

**Date:** 2026-03-25
**Branch:** feature/go-rewrite
**Complexity:** Medium overall (3 small + 2 medium + 1 large step)

---

## Problem Statement

WireGuard kernel counters (`transfer_rx`, `transfer_tx` per peer) reset to zero every time
`wg-quick down` is called — which happens on every `Stop()`, `Restart()`, and container
restart. The frontend currently shows only the live kernel value as both the "current speed"
and the "total bytes" label. After a restart, "total bytes" shows 0 even if hundreds of GB
have passed through the peer historically.

**Goal:** accumulate a running total in SQLite. On each polling tick, compute
`delta = current_kernel_value - last_seen_kernel_value` and add delta to the persisted
total. Expose `totalRx` and `totalTx` as additional fields on the `Peer` JSON so the
frontend can show them as a persistent "lifetime total" label.

---

## Key Facts from Code Research

### Where polling happens
`internal/tunnel/interface.go` — `GetStatus()`, lines 867-923.
Called every 1 second from `Manager.startPolling()` in `manager.go` line 124-141.
`GetStatus()` writes directly into the in-memory `*peer.Peer` struct fields
`TransferRx` and `TransferTx` (runtime-only, not persisted).

### Peer struct (internal/peer/peer.go, lines 40-68)
Runtime fields are clearly separated from persisted fields with a comment:
```
// Runtime fields — populated by TunnelInterface.GetStatus(), NOT persisted.
TransferRx        int64   `json:"transferRx"`
TransferTx        int64   `json:"transferTx"`
```
No total/accumulated fields exist yet.

### Database (internal/db/db.go)
Latest migration: **v10**. Next migration must be **v11**.
The `peers` table does not have any traffic columns today.

### Frontend (internal/frontend/www/js/app.js, lines 2233-2267)
The frontend currently uses `peer.transferRx` as BOTH the speed delta source AND the
"total bytes" display (`bytes(peer.transferTx)` on index.html line 899).
The delta is computed client-side:
```javascript
pp.transferRxCurrent = (peer.transferRx || 0) - pp.transferRxPrevious;
pp.transferRxPrevious = peer.transferRx || 0;
```
The "total" shown is actually the current kernel counter — it goes back to 0 on restart.

The total label is in index.html at lines 899, 910, 1247, 1258, 513, 533:
```html
<br><span class="font-regular" style="font-size:0.85em">{{bytes(peer.transferTx)}}</span>
```

### Locking model in GetStatus()
`GetStatus()` acquires `peersMu.RLock()` for the whole loop (lines 888-923).
Any DB write in the polling path must avoid a write lock on `peersMu` (deadlock risk).
The approach: persist totals asynchronously or with a separate goroutine, not inside
the `RLock` critical section.

---

## Architecture Decision

### Option A: Write to DB on every polling tick (every second)
- Pros: totals always up to date even if the process crashes.
- Cons: 1 UPDATE per peer per second → with 50 peers: 50 writes/s = excessive SQLite WAL pressure.

### Option B: Write to DB periodically (every 30 seconds or on Stop/Restart)
- Pros: minimal write pressure. Totals survive graceful stop.
- Cons: up to 30 seconds of stats lost on crash/SIGKILL.

### Option C: Write on Stop/Restart + periodic flush every 60 seconds
Selected approach. Rationale:
- `Stop()` and `Restart()` always happen before `wg-quick down`, so we can flush totals
  just before the kernel counters disappear.
- A 60-second periodic flush caps crash-loss to 60 seconds, which is acceptable.
- No extra goroutine needed — periodic flush runs in the existing polling goroutine.

### Delta accumulation algorithm

```
// On each GetStatus() tick, for each peer p:
newKernelRx = parse from wg show dump
newKernelTx = parse from wg show dump

delta_rx = max(0, newKernelRx - p.lastSeenRx)   // negative = counter reset (wg restart)
delta_tx = max(0, newKernelTx - p.lastSeenTx)

p.totalRxAccumulated += delta_rx
p.totalTxAccumulated += delta_tx
p.lastSeenRx = newKernelRx
p.lastSeenTx = newKernelTx

p.TransferRx = newKernelRx   // unchanged: still the live kernel value for speed charts
p.TransferTx = newKernelTx
p.TotalRx    = p.totalRxAccumulated  // new: exposed to API
p.TotalTx    = p.totalTxAccumulated
```

**Counter reset detection:** when `newKernelRx < p.lastSeenRx`, the kernel counter has
been reset (interface went down and came back). In this case `delta = max(0, ...)` = 0
for that tick. On the next tick, the delta will be the bytes transferred since restart.
This is the correct behaviour: we do not lose the historical total, we just don't subtract.

### Where to store accumulation state

Two fields are needed per peer at runtime (not yet persisted):
- `lastSeenRx int64` — last kernel counter value (baseline for next delta)
- `lastSeenTx int64` — same for TX

These live on the `*peer.Peer` struct as unexported runtime fields (alongside the existing
`TransferRx`/`TransferTx` pattern), OR as a separate parallel map in `TunnelInterface`.

**Decision: separate map in TunnelInterface.**
Reason: the `Peer` struct is shared with the `peer` package which has no knowledge of
runtime polling state. Adding unexported fields to `peer.Peer` from `tunnel` package
would require a circular import or a messy layering. A `map[string]*peerTrafficState`
inside `TunnelInterface` (same package as `GetStatus()`) is cleaner.

```go
// internal/tunnel/interface.go
type peerTrafficState struct {
    lastSeenRx      int64
    lastSeenTx      int64
    totalRx         int64  // accumulated since ever
    totalTx         int64
    dirty           bool   // true if not yet flushed to DB
}
```

The map is keyed by peer.ID (string). Initialized lazily on first `GetStatus()` tick
for that peer by loading persisted totals from SQLite.

---

## Files to Modify

| File | Change | Complexity |
|------|--------|------------|
| `internal/db/db.go` | Add migration v11: two new columns on `peers` | Small |
| `internal/peer/peer.go` | Add `TotalRx`, `TotalTx` fields + DB read/write helpers | Small |
| `internal/tunnel/interface.go` | Add `trafficMu` + `trafficState` map; rewrite `GetStatus()` delta logic; add `FlushTrafficTotals()`; call flush in `Stop()` and `Restart()` | Large |
| `internal/tunnel/manager.go` | Call `FlushTrafficTotals()` on all interfaces in periodic flush; load totals at startup | Medium |
| `internal/frontend/www/js/app.js` | Use `peer.totalRx` / `peer.totalTx` for the "total bytes" display instead of `peer.transferRx` | Small |
| `internal/frontend/www/index.html` | Update the `bytes(peer.transferTx)` / `bytes(peer.transferRx)` binding to use `totalTx` / `totalRx` | Small |
| `internal/tunnel/tunnel_test.go` | Add tests for delta accumulation logic | Medium |
| `internal/db/db_test.go` | Update `expectedTables` list (already covers `peers`) | Small |

### Files that must NOT be modified

- `internal/peer/peer_test.go` — only tests validation logic unrelated to traffic
- `internal/api/peers.go` — no change needed; `TotalRx`/`TotalTx` are already part of `peer.Peer` JSON, they just need to be non-zero
- Any migration below v11 — never modify existing migrations

---

## Step-by-Step Implementation Plan

### Step 1 — DB migration v11 (Small)

**File:** `/Users/jenya/PycharmProjects/cascade/internal/db/db.go`

Add a new migration entry after the existing `version: 10` block:

```go
{
    version: 11,
    sql: `
-- Persistent traffic totals per peer.
-- Accumulates bytes across wg-quick down/up cycles and container restarts.
-- lastSeenRx/Tx: the most recent kernel counter value (used for delta calc).
-- totalRx/Tx: the running lifetime total in bytes.
ALTER TABLE peers ADD COLUMN last_seen_rx INTEGER NOT NULL DEFAULT 0;
ALTER TABLE peers ADD COLUMN last_seen_tx INTEGER NOT NULL DEFAULT 0;
ALTER TABLE peers ADD COLUMN total_rx     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE peers ADD COLUMN total_tx     INTEGER NOT NULL DEFAULT 0;
`,
},
```

Also update `expectedTables` is not needed (no new table), but the `db_test.go` does not
test column existence — no test change needed for this step.

### Step 2 — Peer struct and DB helpers (Small)

**File:** `/Users/jenya/PycharmProjects/cascade/internal/peer/peer.go`

2a. Add two new JSON-exposed fields to the `Peer` struct under the "Runtime fields" section:

```go
// TotalRx and TotalTx are the accumulated lifetime byte counters, persisted
// in SQLite and updated by the polling loop. Unlike TransferRx/Tx (which reset
// to 0 on wg-quick down), these survive restarts.
TotalRx int64 `json:"totalRx"`
TotalTx int64 `json:"totalTx"`
```

2b. Add a new exported function `SaveTrafficTotals(peerID string, lastSeenRx, lastSeenTx, totalRx, totalTx int64) error`:

```go
func SaveTrafficTotals(peerID string, lastSeenRx, lastSeenTx, totalRx, totalTx int64) error {
    _, err := db.DB().Exec(`
        UPDATE peers
        SET last_seen_rx = ?, last_seen_tx = ?,
            total_rx     = ?, total_tx     = ?
        WHERE id = ?
    `, lastSeenRx, lastSeenTx, totalRx, totalTx, peerID)
    return err
}
```

2c. Add a new exported function `LoadTrafficTotals(peerID string) (lastSeenRx, lastSeenTx, totalRx, totalTx int64, err error)`:

```go
func LoadTrafficTotals(peerID string) (lastSeenRx, lastSeenTx, totalRx, totalTx int64, err error) {
    row := db.DB().QueryRow(`
        SELECT last_seen_rx, last_seen_tx, total_rx, total_tx
        FROM peers WHERE id = ?
    `, peerID)
    err = row.Scan(&lastSeenRx, &lastSeenTx, &totalRx, &totalTx)
    return
}
```

2d. Update `scanPeerRow()` to also scan `total_rx`, `total_tx` from any query that
selects them. However, `GetPeer` and `GetPeers` queries do not select these columns
today. The cleanest approach: add these columns to the SELECT in `GetPeers` and
`GetPeer`, and scan them into `TotalRx`/`TotalTx` directly.

**Important backward compatibility note:** `last_seen_rx`/`last_seen_tx` are internal
implementation details of the polling loop, not exposed to the frontend. They do not
need to be in the `Peer` struct — only `total_rx`/`total_tx` go there. `last_seen_rx`
and `last_seen_tx` are managed exclusively via `SaveTrafficTotals`/`LoadTrafficTotals`.

**Query change in `GetPeers` and `GetPeer`:** add `total_rx, total_tx` to SELECT and
add `Scan` targets. The `scanPeerRow` function signature needs two extra scan targets.
This is a mechanical change, well-contained in peer.go.

### Step 3 — Traffic accumulation in GetStatus() (Large)

**File:** `/Users/jenya/PycharmProjects/cascade/internal/tunnel/interface.go`

3a. Add `peerTrafficState` struct and `trafficState` map to `TunnelInterface`:

```go
type peerTrafficState struct {
    lastSeenRx int64
    lastSeenTx int64
    totalRx    int64
    totalTx    int64
    loaded     bool // true once DB values have been read
    dirty      bool // true if totalRx/Tx changed since last DB flush
}
```

Add to `TunnelInterface` struct:
```go
trafficMu    sync.Mutex                  // protects trafficState map
trafficState map[string]*peerTrafficState // keyed by peer.ID
```

Initialize `trafficState: make(map[string]*peerTrafficState)` in `Create()` and in
`scanInterface()` (wherever `TunnelInterface` is constructed).

3b. Rewrite the inner loop of `GetStatus()` to accumulate deltas:

The current loop (lines 902-922) acquires `peersMu.RLock()` and assigns
`p.TransferRx = rx`. The new logic:
1. Still under `peersMu.RLock()`, read `rx`/`tx` from dump (unchanged).
2. After assigning `p.TransferRx = rx`, acquire `trafficMu.Lock()` briefly to
   update `trafficState[p.ID]`.

**Locking order:** `peersMu.RLock` is acquired first (existing), then `trafficMu.Lock`
inside. This order must be consistent everywhere. Only ever acquire in this order —
never the reverse — to prevent deadlocks.

Pseudocode for the new inner loop body:

```go
// existing:
p.TransferRx = rx
p.TransferTx = tx

// new:
ts := t.getOrLoadTrafficState(p.ID)
if ts != nil {
    if ts.loaded {
        deltaRx := rx - ts.lastSeenRx
        if deltaRx < 0 { deltaRx = 0 }  // kernel reset
        deltaTx := tx - ts.lastSeenTx
        if deltaTx < 0 { deltaTx = 0 }
        ts.totalRx += deltaRx
        ts.totalTx += deltaTx
        if deltaRx > 0 || deltaTx > 0 {
            ts.dirty = true
        }
    }
    ts.lastSeenRx = rx
    ts.lastSeenTx = tx
    ts.loaded = true

    p.TotalRx = ts.totalRx
    p.TotalTx = ts.totalTx
}
```

`getOrLoadTrafficState(peerID)` acquires `trafficMu`, looks up the map, and if not
present, loads from SQLite via `peer.LoadTrafficTotals(peerID)`. Returns the state.

**Important:** `getOrLoadTrafficState` must NOT be called while `peersMu.RLock` is held
if it calls into SQLite under `trafficMu` — both locks held simultaneously is fine as
long as the ordering rule (peersMu then trafficMu) is respected everywhere.

3c. Add `FlushTrafficTotals() error` method on `TunnelInterface`:

```go
// FlushTrafficTotals persists all dirty peer traffic totals to SQLite.
// Called before wg-quick down (Stop/Restart) and periodically.
func (t *TunnelInterface) FlushTrafficTotals() error {
    t.trafficMu.Lock()
    defer t.trafficMu.Unlock()
    for peerID, ts := range t.trafficState {
        if !ts.dirty {
            continue
        }
        if err := peer.SaveTrafficTotals(peerID, ts.lastSeenRx, ts.lastSeenTx, ts.totalRx, ts.totalTx); err != nil {
            log.Printf("tunnel: flush traffic totals %s/%s: %v", t.ID, peerID, err)
            continue
        }
        ts.dirty = false
    }
    return nil
}
```

3d. Call `FlushTrafficTotals()` at the beginning of `Stop()` (before `wg-quick down`):

```go
func (t *TunnelInterface) Stop() error {
    // Persist traffic totals before the kernel resets counters on wg-quick down.
    if err := t.FlushTrafficTotals(); err != nil {
        log.Printf("tunnel: stop %s: flush traffic totals: %v", t.ID, err)
        // non-fatal: continue with stop even if flush fails
    }
    // ... existing stop logic ...
}
```

3e. `Restart()` calls `Stop()` then `Start()` — no additional change needed since
`Stop()` already flushes.

3f. When `GetStatus()` starts (interface is first polled after startup), the
`trafficState` map is empty. The `getOrLoadTrafficState` function will load from SQLite.
The `loaded` field distinguishes "not yet seen this session" from "seen at 0":
- `loaded = false`: skip delta (first tick). Set `lastSeenRx = rx`, `lastSeenTx = tx`,
  `loaded = true`. No delta added (correct: we don't know what was transferred before).
- `loaded = true`: apply delta.

This means: after a container restart, the first polling tick does NOT add a false delta
(it just sets the baseline). Starting from the second tick, delta accumulation works correctly.

### Step 4 — Periodic flush in Manager polling goroutine (Medium)

**File:** `/Users/jenya/PycharmProjects/cascade/internal/tunnel/manager.go`

In `startPolling()`, add a 60-second flush ticker alongside the existing 1-second status
ticker:

```go
func (m *Manager) startPolling() {
    go func() {
        ticker   := time.NewTicker(time.Second)
        flushTkr := time.NewTicker(60 * time.Second)
        defer ticker.Stop()
        defer flushTkr.Stop()
        for {
            select {
            case <-ticker.C:
                m.mu.RLock()
                for _, t := range m.interfaces {
                    t.GetStatus()
                }
                m.mu.RUnlock()
            case <-flushTkr.C:
                m.mu.RLock()
                for _, t := range m.interfaces {
                    if err := t.FlushTrafficTotals(); err != nil {
                        log.Printf("tunnel: periodic flush %s: %v", t.ID, err)
                    }
                }
                m.mu.RUnlock()
            case <-m.stopCh:
                // Final flush on graceful shutdown.
                m.mu.RLock()
                for _, t := range m.interfaces {
                    _ = t.FlushTrafficTotals()
                }
                m.mu.RUnlock()
                return
            }
        }
    }()
}
```

### Step 5 — Frontend: use totalRx/totalTx for lifetime display (Small)

**Files:**
- `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/index.html`
- `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/js/app.js`

5a. In `index.html`, the "total bytes" spans currently read `bytes(peer.transferTx)` and
`bytes(peer.transferRx)`. These occur at lines 899, 910, 1247, 1258 (and for legacy
clients at 513, 533). Change each occurrence to `bytes(peer.totalTx)` and
`bytes(peer.totalRx)` respectively.

The `:title` tooltip attributes also reference `bytes(peer.transferTx)` — update those
to use `totalTx`/`totalRx`.

5b. In `app.js`, the `peersPersist` delta calculation uses `peer.transferRx` for speed
charts — this must NOT be changed. The speed charts measure rate-of-change in the kernel
counter (which is correct; it resets to 0 but the delta between consecutive ticks is
still meaningful for speed). Only the static "total" label in the template uses the new
`totalRx`/`totalTx` fields.

No change to the delta calculation logic in `app.js` is needed.

5c. The `transferRxPrevious` initialization in `peersPersist` is still correctly seeded
from `peer.transferRx || 0` — no change needed.

5d. Backward compatibility: for peers that have never had any traffic or whose
`total_rx`/`total_tx` are 0 in the DB, `peer.totalRx` = 0 from the API. The template's
`v-if="peer.transferTx"` guards (lines 892, 903, 1240, 1251) currently hide the stats
block when `transferTx` is 0. After migration, we also need the block to appear when
`totalTx > 0` even if current `transferTx` is 0 (i.e. just restarted). Update those
guards to `v-if="peer.transferTx || peer.totalTx"`.

### Step 6 — Tests (Medium)

**File:** `/Users/jenya/PycharmProjects/cascade/internal/tunnel/tunnel_test.go`

6a. `TestGetStatus_DeltaAccumulation`:
- Create a `TunnelInterface` with `trafficState` initialized.
- Manually call the delta accumulation logic (extract to a private helper
  `applyTrafficDelta(peerID string, rx, tx int64)` to make it unit-testable without
  requiring a real `wg show` call).
- Verify: first call sets baseline (totalRx=0), second call with higher values adds delta,
  third call with reset-to-zero values (simulating wg restart) does not subtract.

6b. `TestGetStatus_CounterResetIsIgnored`:
- Simulate: lastSeenRx=1000, new rx=50 (counter reset). Verify delta=0, totalRx unchanged.

6c. `TestFlushTrafficTotals_OnlyDirtyPeersFlushed`:
- Requires in-memory SQLite (use `db.Init(t.TempDir())`).
- Insert a peer row, set ts.dirty=false, call FlushTrafficTotals → verify no DB write.
- Set ts.dirty=true, call FlushTrafficTotals → verify DB updated.

6d. `TestFlushTrafficTotals_CalledBeforeStop`:
- This is behavioral — hard to test without exec(). Document as an integration concern.
  Add a comment in the test file explaining why it is covered by the Stop() code review.

**File:** `/Users/jenya/PycharmProjects/cascade/internal/db/db_test.go`

6e. Add migration v11 check: after `Init()`, verify that `peers` table has the four new
columns. Can use `PRAGMA table_info(peers)` to enumerate columns.

---

## Edge Cases

| Edge Case | Handling |
|-----------|----------|
| Counter reset (wg-quick down+up within session) | `delta = max(0, new - lastSeen)` — skip negative delta. Correct. |
| Container restart (all in-memory state lost) | `LoadTrafficTotals` on first poll tick reads persisted totals from SQLite. First tick only sets baseline, no false delta. |
| Peer deleted while traffic is running | `FlushTrafficTotals` iterates `trafficState`; if peer was deleted, `SaveTrafficTotals` will silently fail (UPDATE 0 rows). No error. The map entry is orphaned in memory — clean up by checking if peer still exists in `peersMu` map, or by clearing `trafficState[peerID]` in `RemovePeer()`. |
| Peer re-enabled after being disabled | When disabled, `GetStatus()` skips the peer (it does not appear in `wg show dump`). `lastSeenRx` stays at the last value before disable. When re-enabled, the kernel starts fresh at 0 — first tick: delta = max(0, 0 - lastSeenRx) = 0. Correct. |
| Interface stopped manually (Stop()) | `FlushTrafficTotals()` called in `Stop()` before `wg-quick down`. Totals flushed. |
| AWG2 kernel mode Restart() in KernelRemovePeer | `Restart()` calls `Stop()` which calls `FlushTrafficTotals()`. Correct. |
| AWG2 userspace Reload() in KernelRemovePeer | Reload does NOT call Stop — kernel counters are NOT reset by syncconf. `trafficState.lastSeenRx` remains valid. No flush needed. Correct. |
| Concurrent GetStatus() and FlushTrafficTotals() | Both protected by `trafficMu`. GetStatus acquires `peersMu.RLock` first, then `trafficMu.Lock`. FlushTrafficTotals acquires only `trafficMu.Lock`. No deadlock. |
| Concurrent Stop() and GetStatus() polling | Stop() calls FlushTrafficTotals (trafficMu), then wg-quick down. GetStatus may be running concurrently (peersMu.RLock). No shared state conflict — they use different locks. After wg-quick down, GetStatus returns empty output (interface gone). |
| Very long-running peer (> 2^63 bytes) | int64 max is ~9.2 EB. WireGuard kernel counter is uint64, so wraps at 2^64. The `peer.Peer` fields are int64. If a peer ever transfers >9.2 EB, totalRx overflows. Acceptable: document as known limitation (physically unreachable in practice). |
| Multiple peers on same interface, one crashes | Each peer has its own `trafficState` entry. One peer's failure does not affect others. |
| DB unavailable during flush | `SaveTrafficTotals` returns error, logged. Flush continues for other peers. `ts.dirty` remains true — next flush attempt will retry. |

---

## Risks and Breaking Changes

1. **Migration v11 adds 4 columns to `peers` table.** Existing rows get DEFAULT 0. This
   is backward-compatible — old totals are lost (expected: first run after upgrade starts
   fresh). No data loss of existing peer configuration.

2. **`scanPeerRow` signature change.** The SELECT in `GetPeers`/`GetPeer` is extended.
   This is internal — no API contract change. The `peer.Peer` JSON gains two new fields
   (`totalRx`, `totalTx`) but since JSON is additive, old frontends ignore them.

3. **Frontend change to `bytes(peer.totalTx)` instead of `bytes(peer.transferTx)`.**
   If the backend is upgraded but the frontend is cached (old CDN), the total display
   shows 0 until cache refresh. Non-breaking: the peer card remains functional.

4. **`Stop()` now makes a DB write before `wg-quick down`.** This adds a few milliseconds
   to stop latency. Given SQLite WAL mode and the fact that Stop is not on a hot path,
   this is acceptable.

5. **`getOrLoadTrafficState` can call SQLite while `peersMu.RLock` is held.** This means
   the first poll tick for a peer briefly holds two locks. Given SQLite busy_timeout=5s
   and the fact that migrations and Stop() are not frequent, this is low-risk. If this
   becomes a concern, load can be done outside `peersMu` by pre-loading on `LoadPeers()`.

   **Safer alternative:** pre-populate `trafficState` from `LoadPeers()` in `LoadInterface()`
   rather than lazily in `GetStatus()`. This eliminates the SQLite call inside the
   polling lock. Recommended.

---

## Recommended Implementation Order

1. Step 1 — DB migration v11 (commit alone: "db: migration v11 — persistent traffic totals")
2. Step 2 — peer.go additions (commit: "peer: add TotalRx/TotalTx fields + SaveTrafficTotals/LoadTrafficTotals")
3. Step 3 — interface.go GetStatus() rewrite + FlushTrafficTotals (commit: "tunnel: accumulate traffic deltas in GetStatus, flush on Stop/Restart")
4. Step 6e — db_test.go column check (included with Step 1 commit)
5. Step 4 — manager.go periodic flush (commit: "tunnel: periodic 60s traffic flush in polling goroutine")
6. Step 5 — frontend changes (commit: "frontend: display persistent totalRx/totalTx as lifetime bytes")
7. Step 6a-d — tunnel tests (commit: "test: traffic delta accumulation and flush logic")

---

## Files Modified Summary

| File | Type of Change |
|------|---------------|
| `/Users/jenya/PycharmProjects/cascade/internal/db/db.go` | Add migration v11 (4 columns on peers) |
| `/Users/jenya/PycharmProjects/cascade/internal/db/db_test.go` | Add PRAGMA table_info check for new columns |
| `/Users/jenya/PycharmProjects/cascade/internal/peer/peer.go` | Add TotalRx/TotalTx to struct; add SaveTrafficTotals/LoadTrafficTotals; extend SELECT in GetPeers/GetPeer |
| `/Users/jenya/PycharmProjects/cascade/internal/tunnel/interface.go` | Add peerTrafficState + trafficMu + trafficState; rewrite GetStatus inner loop; add FlushTrafficTotals; call flush in Stop() |
| `/Users/jenya/PycharmProjects/cascade/internal/tunnel/manager.go` | Add 60s flush ticker in startPolling; final flush in stopCh case |
| `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/index.html` | Change bytes(peer.transferTx/Rx) to bytes(peer.totalTx/Rx); update v-if guards |
| `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/js/app.js` | No change to delta logic; no change needed (totalRx/Tx appear automatically in JSON) |
| `/Users/jenya/PycharmProjects/cascade/internal/tunnel/tunnel_test.go` | Add 3 new test functions for delta, reset, and flush |

## Files That Must NOT Be Modified

| File | Reason |
|------|--------|
| `internal/peer/peer_test.go` | Only tests validation; no traffic logic |
| `internal/api/peers.go` | No API change — new fields appear via existing JSON serialization |
| Any existing migration (v1-v10) | NEVER modify existing migrations |
| `internal/frontend/www/js/app.js` (delta logic, lines 2242-2246) | Speed chart delta must remain keyed on `transferRx`/`transferTx` |
