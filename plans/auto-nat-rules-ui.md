# Plan: Auto NAT Rules — Display and Override in UI

**Date:** 2026-04-02
**Status:** Draft
**Estimated effort:** 5–7 hours

---

## 1. Current State Analysis

### 1.1 How NAT is generated today (Go rewrite)

Every WireGuard interface runs `generateWgConfig()` in
`internal/tunnel/interface.go`. For any interface that has an `Address` set,
the method unconditionally appends a PostUp/PostDown line containing:

```
iptables-nft -t nat -A POSTROUTING -s <subnet> -o $ISP -j MASQUERADE
```

Key observations:
- MASQUERADE is added for **all** interfaces regardless of `DisableRoutes`
  (S2S interconnect interfaces also get NAT — this is already a latent bug per
  CLAUDE.md FIX-1 comment block for `disableRoutes=true`).
- The `$ISP` variable is resolved dynamically at `wg-quick up` time via
  `ip -4 route show default | awk 'NR==1{print $5}'`.
- The rule is ephemeral: it exists only while the interface is up. It is
  removed by PostDown when the interface stops.

### 1.2 How NatManager works

`internal/nat/manager.go` manages **user-defined** NAT rules stored in SQLite
(`nat_rules` table). These are independent of WireGuard PostUp lines. On
`RestoreAll()` they are applied via `iptables-nft -t nat -A POSTROUTING`.

There is **no coupling** between `NatManager` and `TunnelInterface` today.

### 1.3 What the UI expects

`internal/frontend/www/index.html` already renders auto rules:
- Column `rule.auto` — if true, shows an "auto" badge and a "→ iface" button
  instead of Edit/Delete.
- `rule.interfaceId` — used by `goToInterface()` to navigate.
- The enable/disable dot is non-clickable for auto rules.

`internal/frontend/www/js/app.js` already has `goToInterface()` and the
`_natRuleSourceLabel`, `_natRuleTypeLabel` helpers. No frontend changes for
basic display are needed.

### 1.4 What the API returns today

`GET /api/nat/rules` calls `nat.Get().GetRules()` — returns only DB rows.
Auto rules from PostUp are **never returned**. The ARCHITECTURE.md comment
"also returns auto-rules" describes the Node.js intent, not the current Go
implementation.

The `NatRule` struct has no `auto` or `interfaceId` field, so the frontend's
`rule.auto` check always evaluates to `undefined` (falsy) — all rules show
Edit/Delete buttons.

### 1.5 Latent bug: MASQUERADE on DisableRoutes=true interfaces

`generateWgConfig()` adds MASQUERADE unconditionally (no `!t.DisableRoutes`
guard). CLAUDE.md FIX-1 documents that `disableRoutes=true` (S2S interconnect)
should NOT get MASQUERADE. This bug should be fixed as part of this work.

---

## 2. Architecture Decision

### Option A: PostUp stays, NatManager reads interfaces to build virtual auto rules

- PostUp/PostDown continue to manage the kernel rule.
- `GET /api/nat/rules` merges DB rules with "virtual" auto rules constructed
  in-memory by reading `tunnel.Get().GetAllInterfaces()`.
- Auto rules are read-only (no toggle, no edit), but the user can see them.
- To disable NAT on an interface, a new flag `natDisabled bool` is added to
  `TunnelInterface`. When true, `generateWgConfig()` omits the MASQUERADE line.

### Option B: PostUp removed entirely; NatManager fully controls NAT

- Remove MASQUERADE from PostUp/PostDown.
- On interface start/stop, NatManager is called to apply/remove the auto rule.
- Auto rules are stored in `nat_rules` table with an `interface_id` column.
- Users can toggle/delete them.

### Option C: Flag `natEnabled bool` on the interface

- PATCH `/api/tunnel-interfaces/:id` accepts `natEnabled`.
- `generateWgConfig()` checks `!t.NatEnabled` before adding MASQUERADE.
- UI shows a toggle on the interface card.
- No changes to NatManager or the NAT page.

### Recommendation: Option A (hybrid approach)

**Rationale:**

Option B is the most invasive. Removing MASQUERADE from PostUp breaks the
clean, self-contained wg-quick config file. It also requires NatManager to be
called from TunnelInterface start/stop lifecycle hooks, creating a circular
dependency between packages (tunnel imports nat, nat imports tunnel is a cycle).
PostDown auto-cleanup becomes manual.

Option C is the least invasive but provides the poorest UX: the user has to
navigate to the Interfaces page and find the flag. It also duplicates logic
(NAT is managed in two places: flag on interface, and NatManager rules).

Option A preserves the clean PostUp architecture, uses the existing NAT page as
the single place for NAT management, and needs minimal new code. The
`natDisabled` flag is the opt-out mechanism. Auto rules remain ephemeral (not
in DB), shown read-only. Disabling happens by setting `natDisabled=true` which
simply makes `generateWgConfig()` skip MASQUERADE — PostDown still cleans up
whatever was in the kernel (with `2>/dev/null || true`).

**User flow with Option A:**
1. User opens NAT page — sees all user-defined rules plus auto rules from
   active interfaces (marked "auto").
2. User wants to disable NAT for wg10: clicks "→ iface" button on the auto
   row, or navigates to wg10 and uses a "Disable NAT" toggle.
3. Toggle sets `natDisabled=true` on the interface and calls `Restart()` so
   the new config takes effect.
4. On next NAT page load, wg10's auto rule no longer appears (because
   `natDisabled=true`).

---

## 3. Implementation Plan

### Step 1: Fix the DisableRoutes=true NAT bug [Small — 30 min]

**File:** `internal/tunnel/interface.go` → `generateWgConfig()`

Currently the PostUp/PostDown block has no condition on `t.DisableRoutes`.
Change the outer condition so MASQUERADE is only added when
`!t.DisableRoutes`:

```
Current:  if t.Address != "" { ... always adds MASQUERADE ... }
Target:   if t.Address != "" { ... FORWARD always; MASQUERADE only if !t.DisableRoutes }
```

Concretely: split the single PostUp string into two. The FORWARD rules
(`-A FORWARD -i/-o`) apply regardless. The MASQUERADE rule is wrapped in
`if !t.DisableRoutes`.

Same for PostDown.

Risk: low — this is a pure config-generation change. Existing S2S interfaces
that were incorrectly getting MASQUERADE will lose it on next restart. If they
need NAT for some reason, users can add an explicit rule via NatManager.

Complexity: Small.

### Step 2: Add `NatDisabled bool` field to TunnelInterface [Small — 45 min]

**Files:**
- `internal/tunnel/interface.go` — add field, update `save()`, `scanInterface()`
- `internal/db/db.go` — add migration v12

**DB migration v12:**
```sql
ALTER TABLE interfaces ADD COLUMN nat_disabled INTEGER NOT NULL DEFAULT 0;
```

`DEFAULT 0` means all existing interfaces keep their current auto-NAT behavior.

**`TunnelInterface` struct addition:**
```go
NatDisabled bool `json:"natDisabled"` // when true, PostUp omits MASQUERADE
```

**`InterfaceUpdate` struct addition:**
```go
NatDisabled *bool
```

**`Update()` method:** add handling for `upd.NatDisabled`.

**`save()` and `scanInterface()`:** add the new column to INSERT/SELECT/UPDATE.

Complexity: Small.

### Step 3: Expose `natDisabled` in the PATCH API [Small — 30 min]

**File:** `internal/api/interfaces.go`

In `updateInterface` handler, parse `natDisabled` from the request body and
pass it to `InterfaceUpdate`. The handler already uses a `map[string]any`
for raw parsing; add logic to extract the `natDisabled` bool.

Also expose `natDisabled` in `ifaceJSON()` so the frontend can read the
current state.

No new endpoint needed — PATCH `:id` already covers this.

Complexity: Small.

### Step 4: Modify `generateWgConfig()` to respect `NatDisabled` [Small — 20 min]

**File:** `internal/tunnel/interface.go`

Change the PostUp/PostDown generation to check `t.NatDisabled`:

```
if !t.NatDisabled && !t.DisableRoutes {
    // append MASQUERADE to PostUp
    // append MASQUERADE removal to PostDown
}
```

This is the actual enforcement. After a `Restart()` the new config is applied.

Complexity: Small.

### Step 5: Add `GetAutoNatRules()` to NatManager [Medium — 1.5 hours]

**File:** `internal/nat/manager.go`

Add a new method that takes the list of all interfaces and returns a slice
of `NatRule`-like structs representing the auto rules from PostUp:

```go
type AutoNatRule struct {
    NatRule              // embeds all display fields
    Auto        bool   `json:"auto"`
    InterfaceID string `json:"interfaceId"`
}
```

The method iterates each interface. If `t.Enabled && t.Address != "" &&
!t.NatDisabled && !t.DisableRoutes`, it builds a virtual rule:
- `Name`: interface name + " (auto)"
- `Source`: the subnet derived from `t.Address` (e.g. "10.8.0.0/24")
- `OutInterface`: "$ISP" (runtime, display as "(runtime)")
- `Type`: "MASQUERADE"
- `Enabled`: true (always — the rule is active while the interface is up)
- `Auto`: true
- `InterfaceID`: t.ID

The method signature requires `[]*TunnelInterface` as input to avoid an
import cycle between `nat` and `tunnel` packages.

**Important:** The `nat` package must NOT import `tunnel`. The handler in
`internal/api/nat.go` will pass the interface list from `tunnel.Get()`.

Complexity: Medium.

### Step 6: Update `GET /api/nat/rules` to merge auto rules [Small — 45 min]

**File:** `internal/api/nat.go`

Modify `getNatRules` to:
1. Call `nat.Get().GetRules()` for user-defined rules.
2. Call `tunnel.Get().GetAllInterfaces()` to get the interface list.
3. Call `nat.Get().GetAutoNatRules(interfaces)` to get virtual auto rules.
4. Merge: auto rules first (ordered by interface CreatedAt), then user rules.
5. Return `{ rules: [...all...] }`.

The response type must accommodate both `NatRule` and `AutoNatRule`. Options:
- Define a unified `NatRuleView` struct with `Auto bool` and `InterfaceID string`
  added, marshalled together.
- Use `fiber.Map` slice.

Recommended: Add `Auto bool` and `InterfaceID string` directly to `NatRule`
with `json:"auto,omitempty"` and `json:"interfaceId,omitempty"` — zero values
are omitted, preserving the existing JSON contract for DB rules. Auto rules
are constructed in-memory with `Auto=true`.

Complexity: Small.

### Step 7: Add "Disable NAT" toggle to Interface UI [Medium — 1.5 hours]

**Files:**
- `internal/frontend/www/index.html` — interface info card
- `internal/frontend/www/js/app.js` — `openInterfaceEdit`, `saveInterfaceEdit`

In the Edit Interface modal, add a checkbox "Disable auto NAT (MASQUERADE)".
Bound to `interfaceEdit.natDisabled`.

In `saveInterfaceEdit()`, include `natDisabled` in the PATCH body. If the
value changes, the backend will call `Update()` which triggers
`RegenerateConfig()` + `Reload()` (syncconf). Since MASQUERADE is in PostUp
(not syncconf), a full `Restart()` is needed when `natDisabled` changes.

**Backend consideration for Step 3/6:** When `NatDisabled` changes in
`Update()`, if the interface is currently enabled, call `Restart()` instead
of `Reload()`. `Reload()` uses `syncconf` which skips PostUp/PostDown.
`Restart()` does `down → up` so the new PostUp (without MASQUERADE) is applied.

Add this logic to `TunnelInterface.Update()`:
- If `upd.NatDisabled != nil && *upd.NatDisabled != old_value && t.Enabled`:
  call `t.Restart()` after `save()` and `RegenerateConfig()`.

Complexity: Medium.

### Step 8: Frontend — Disable NAT toggle display in NAT page [Small — 30 min]

**File:** `internal/frontend/www/index.html`

The NAT page table already handles `rule.auto` correctly (badge, read-only dot,
"→ iface" button). No changes needed for display.

However, add a tooltip or small note to the auto rule row explaining how to
disable it (e.g., "Edit interface to disable"). This improves discoverability.

Complexity: Small.

---

## 4. Files to Modify

| File | Change | Step |
|------|--------|------|
| `internal/tunnel/interface.go` | Fix DisableRoutes NAT bug; add `NatDisabled` field; update `save()`, `scanInterface()`, `Update()`, `generateWgConfig()` | 1, 2, 4 |
| `internal/db/db.go` | Add migration v12: `nat_disabled` column on `interfaces` | 2 |
| `internal/nat/manager.go` | Add `AutoNatRule` type and `GetAutoNatRules(ifaces)` method | 5 |
| `internal/api/nat.go` | Merge auto rules in `getNatRules` handler | 6 |
| `internal/api/interfaces.go` | Parse `natDisabled` in `updateInterface`; expose in `ifaceJSON` | 3 |
| `internal/frontend/www/index.html` | Add "Disable auto NAT" checkbox to Edit Interface modal; optional tooltip on auto NAT rows | 7, 8 |
| `internal/frontend/www/js/app.js` | Add `natDisabled` to `interfaceEdit` data; include in `saveInterfaceEdit()` | 7 |

### Files that must NOT be modified

- `internal/db/db.go` existing migrations (1–11) — never modify, only add new migration
- `internal/nat/nat_test.go` — do not break existing tests; extend with new tests
- `internal/tunnel/manager.go` — no changes needed

---

## 5. Risks and Edge Cases

### Risk 1: Import cycle nat ↔ tunnel
The `nat` package must not import `tunnel`. `GetAutoNatRules` takes
`[]*tunnel.TunnelInterface` as parameter — this direction (tunnel imported by
nat) is a cycle. Solution: define a minimal interface in `nat` package:

```go
type IfaceInfo struct {
    ID          string
    Name        string
    Address     string
    Enabled     bool
    NatDisabled bool
    DisableRoutes bool
}
```

The API handler converts `*tunnel.TunnelInterface` to `[]nat.IfaceInfo` before
calling `GetAutoNatRules`. This avoids the import cycle entirely.

### Risk 2: `OutInterface` is dynamic ($ISP)
The auto rule's `OutInterface` is resolved at runtime by `wg-quick up`. The
NAT table cannot show the real interface name. Display as `$ISP (runtime)`.
The existing UI already handles this: `<span v-if="rule.auto" class="text-xs
text-gray-400"> (runtime)</span>` is in the template next to the outInterface.

### Risk 3: MASQUERADE needs Restart, not Reload
When `natDisabled` changes, `syncconf` (Reload) is insufficient — it skips
PostUp/PostDown. The change requires a full `wg-quick down + up` (Restart).
The `Update()` method must detect this change and call `Restart()` instead of
`Reload()`. Risk: brief downtime (~1s) for peers on that interface.

### Risk 4: Auto rule visibility when interface is stopped
Auto rules should only appear for enabled (running) interfaces, since the
iptables rule only exists while the interface is up. Filter: include auto rules
only where `t.Enabled == true`.

### Risk 5: Existing S2S interfaces incorrectly got MASQUERADE
After Step 1 (DisableRoutes fix), S2S interfaces will no longer get
MASQUERADE on next restart. This is the correct behavior but may surprise users
who inadvertently relied on it. The change is backward-compatible in the sense
that S2S traffic was never supposed to be NATted.

### Risk 6: Test coverage
The new `GetAutoNatRules` method is pure in-memory logic (no kernel calls).
It must be covered by unit tests in `internal/nat/nat_test.go`. Add tests for:
- Empty interface list → empty auto rules
- Enabled interface with address → one auto rule
- Enabled interface with `NatDisabled=true` → no auto rule
- Enabled interface with `DisableRoutes=true` → no auto rule
- Stopped interface → no auto rule

---

## 6. Backward Compatibility

- The `nat_rules` table is extended via a new migration — safe (DEFAULT 0).
- The `NatRule` JSON adds optional fields `auto` and `interfaceId` with
  `omitempty` — existing DB-backed rules return without these fields.
- The frontend's `rule.auto` check was already in place — the fix activates
  functionality that was already designed.
- No existing API endpoints are removed or renamed.
- MASQUERADE behavior is unchanged for interfaces where `natDisabled=false`
  (the default) and `disableRoutes=false`.

---

## 7. Summary: Recommended Variant and Steps

**Recommended:** Option A (hybrid — PostUp stays, virtual auto rules in API,
flag on interface to opt out).

**Step sequence and complexity:**

| # | Step | Complexity | Hours |
|---|------|-----------|-------|
| 1 | Fix DisableRoutes NAT bug in generateWgConfig | Small | 0.5 |
| 2 | Add `nat_disabled` to DB (migration v12) + TunnelInterface struct | Small | 0.75 |
| 3 | Expose `natDisabled` in PATCH API + `ifaceJSON` | Small | 0.5 |
| 4 | `generateWgConfig()` respects `NatDisabled` | Small | 0.25 |
| 5 | `GetAutoNatRules([]IfaceInfo)` in NatManager | Medium | 1.5 |
| 6 | Merge auto rules in `GET /api/nat/rules` | Small | 0.75 |
| 7 | UI: "Disable auto NAT" checkbox in Edit Interface modal | Medium | 1.5 |
| 8 | UI: tooltip on auto NAT rows in NAT page | Small | 0.25 |
| — | Unit tests for GetAutoNatRules | Small | 0.5 |

**Total estimated: 6–7 hours**

Steps 1–4 are independent of each other and can be done in parallel.
Steps 5–6 depend on each other (step 6 calls step 5's method).
Steps 7–8 depend on steps 2–3 (need the `natDisabled` field in the API).
