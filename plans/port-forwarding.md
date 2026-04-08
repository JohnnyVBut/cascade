# Port Forwarding (DNAT) — Implementation Plan

**Branch:** `feature/port-forwarding`
**Date:** 2026-04-08
**Estimated total:** ~9.5 hours

---

## 1. Goal and Context

Transparent traffic cascading via iptables PREROUTING DNAT. A client connects to the RU server on a given port; the RU server rewrites the destination and forwards the packet to the NL server.

The three iptables rules required for each DNAT rule (protocol `udp`, inPort `51820`, destIP `NL_IP`):

```bash
# 1. Rewrite destination (PREROUTING)
iptables-nft -t nat -A PREROUTING -p udp --dport 51820 -j DNAT --to-destination NL_IP:51820

# 2. Allow forwarded NEW+ESTABLISHED traffic toward destination
iptables-nft -A FORWARD -p udp -d NL_IP --dport 51820 -m state --state NEW,ESTABLISHED,RELATED -j ACCEPT

# 3. Allow return traffic (ESTABLISHED only)
iptables-nft -A FORWARD -p udp -s NL_IP --sport 51820 -m state --state ESTABLISHED,RELATED -j ACCEPT
```

Rule 1 is in the `nat` table. Rules 2 and 3 are in the `filter` table, FORWARD chain. MASQUERADE for the outbound leg is already handled by the existing `NatManager` (auto-rule from tunnel interface PostUp).

---

## 2. Data Model

### Migration v13 — new table `dnat_rules`

```sql
CREATE TABLE dnat_rules (
    id         TEXT    PRIMARY KEY,
    name       TEXT    NOT NULL DEFAULT '',
    protocol   TEXT    NOT NULL DEFAULT 'udp',   -- 'tcp' | 'udp' | 'both'
    in_port    INTEGER NOT NULL DEFAULT 0,
    dest_ip    TEXT    NOT NULL DEFAULT '',
    dest_port  INTEGER NOT NULL DEFAULT 0,        -- 0 = same as in_port
    comment    TEXT    NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
```

Notes:
- `dest_port = 0` is the sentinel "same as in_port" — the manager uses `in_port` when `dest_port` is 0.
- No `order_idx` needed: DNAT rules are independent (unlike firewall rules that need ordered evaluation).
- `protocol = 'both'` expands to two separate iptables rules (one for tcp, one for udp).

---

## 3. API Contract

All endpoints live under `/api/nat/dnat`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/nat/dnat` | List all DNAT rules → `{ rules: [...] }` |
| `POST` | `/api/nat/dnat` | Create rule → `201 { rule: {...} }` |
| `PATCH` | `/api/nat/dnat/:id` | Full update OR toggle `{ enabled: bool }` → `{ rule: {...} }` |
| `DELETE` | `/api/nat/dnat/:id` | Delete → `204` |

### DnatRule JSON shape

```json
{
  "id":        "uuid",
  "name":      "Forward WG port to NL",
  "protocol":  "udp",
  "inPort":    51820,
  "destIp":    "10.100.0.1",
  "destPort":  51820,
  "comment":   "Cascade RU → NL",
  "enabled":   true,
  "createdAt": "2026-04-08T10:00:00Z"
}
```

### DnatRuleInput (create/update body)

```json
{
  "name":     "Forward WG port to NL",
  "protocol": "udp",
  "inPort":   51820,
  "destIp":   "10.100.0.1",
  "destPort": 51820,
  "comment":  ""
}
```

`destPort` is optional; if omitted/0, it defaults to `inPort` when stored.

---

## 4. iptables Command Generation

For a rule `{protocol:"udp", inPort:51820, destIp:"10.100.0.1", destPort:51820}`:

```
Action A (append):
  iptables-nft -t nat -A PREROUTING -p udp --dport 51820 -j DNAT --to-destination 10.100.0.1:51820
  iptables-nft -A FORWARD -p udp -d 10.100.0.1 --dport 51820 -m state --state NEW,ESTABLISHED,RELATED -j ACCEPT
  iptables-nft -A FORWARD -p udp -s 10.100.0.1 --sport 51820 -m state --state ESTABLISHED,RELATED -j ACCEPT

Action D (delete, 2>/dev/null || true style — ignore already-gone):
  iptables-nft -t nat -D PREROUTING -p udp --dport 51820 -j DNAT --to-destination 10.100.0.1:51820
  iptables-nft -D FORWARD -p udp -d 10.100.0.1 --dport 51820 -m state --state NEW,ESTABLISHED,RELATED -j ACCEPT
  iptables-nft -D FORWARD -p udp -s 10.100.0.1 --sport 51820 -m state --state ESTABLISHED,RELATED -j ACCEPT

Action C (check, for idempotency before append):
  iptables-nft -t nat -C PREROUTING -p udp --dport 51820 -j DNAT --to-destination 10.100.0.1:51820
  iptables-nft -C FORWARD -p udp -d 10.100.0.1 --dport 51820 -m state --state NEW,ESTABLISHED,RELATED -j ACCEPT
  iptables-nft -C FORWARD -p udp -s 10.100.0.1 --sport 51820 -m state --state ESTABLISHED,RELATED -j ACCEPT
```

When `protocol = "both"`, all three commands are emitted twice: once with `-p tcp`, once with `-p udp`.

The effective destPort is `rule.DestPort` if non-zero, otherwise `rule.InPort`.

---

## 5. Step-by-Step Implementation Plan

### Step 1 — DB Migration v13 [Small — 0.5 h]

**File:** `/Users/jenya/PycharmProjects/cascade/internal/db/db.go`

Add a new entry to the `migrations` slice after the existing v12 entry (line 391 in `db.go`):

```go
{
    version: 13,
    sql: `
-- DNAT (Port Forwarding) rules.
-- protocol: 'tcp' | 'udp' | 'both'
-- dest_port = 0 means same as in_port.
CREATE TABLE dnat_rules (
    id         TEXT    PRIMARY KEY,
    name       TEXT    NOT NULL DEFAULT '',
    protocol   TEXT    NOT NULL DEFAULT 'udp',
    in_port    INTEGER NOT NULL DEFAULT 0,
    dest_ip    TEXT    NOT NULL DEFAULT '',
    dest_port  INTEGER NOT NULL DEFAULT 0,
    comment    TEXT    NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
`,
},
```

No backward-compatibility risk: pure addition of a new table.

---

### Step 2 — DNAT Manager [Large — 3.5 h]

**New file:** `/Users/jenya/PycharmProjects/cascade/internal/nat/dnat.go`

The file lives in the existing `nat` package so it shares the helpers `boolInt`, `strIf`, `isIPv4Addr`, `isIPOrCIDR`, and access to `util.ExecDefault` / `util.ExecSilent`.

#### 2a. Types

```go
// DnatRule is stored in the dnat_rules table.
type DnatRule struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    Protocol  string `json:"protocol"`   // "tcp" | "udp" | "both"
    InPort    int    `json:"inPort"`
    DestIP    string `json:"destIp"`
    DestPort  int    `json:"destPort"`   // 0 = same as InPort
    Comment   string `json:"comment"`
    Enabled   bool   `json:"enabled"`
    CreatedAt string `json:"createdAt"`
}

// DnatRuleInput is the create/update payload from the HTTP handler.
type DnatRuleInput struct {
    Name     string `json:"name"`
    Protocol string `json:"protocol"`
    InPort   int    `json:"inPort"`
    DestIP   string `json:"destIp"`
    DestPort int    `json:"destPort"`
    Comment  string `json:"comment"`
}
```

#### 2b. Public methods on `*Manager`

Add to the existing `Manager` struct (no new struct needed):

| Method | Signature | Notes |
|--------|-----------|-------|
| `GetDnatRules` | `() ([]DnatRule, error)` | SELECT ordered by created_at |
| `GetDnatRule` | `(id string) (*DnatRule, error)` | nil if not found |
| `AddDnatRule` | `(inp DnatRuleInput) (*DnatRule, error)` | validate → applyDnat → INSERT |
| `UpdateDnatRule` | `(id string, inp DnatRuleInput) (*DnatRule, error)` | remove old → apply new |
| `DeleteDnatRule` | `(id string) error` | remove from kernel + DELETE |
| `ToggleDnatRule` | `(id string, enabled bool) (*DnatRule, error)` | apply/remove from kernel |
| `RestoreAllDnat` | `()` | called from `RestoreAll()` — applies all enabled DNAT rules |

#### 2c. Private helpers in `dnat.go`

- `validateDnat(inp DnatRuleInput) error` — checks name non-empty, protocol in {tcp,udp,both}, inPort 1-65535, destIP is valid IPv4, destPort 0 or 1-65535; uses `isIPv4Addr`.
- `buildDnatCmds(rule *DnatRule, action string) []string` — returns slice of iptables-nft command strings for the given action (`A`, `D`, `C`). Expands `protocol="both"` to duplicate commands. Uses `effectiveDest(rule)` for the `--to-destination` value.
- `effectiveDest(rule *DnatRule) string` — returns `"IP:port"`, using `InPort` when `DestPort == 0`.
- `applyDnat(rule *DnatRule) error` — runs `buildDnatCmds(rule, "C")` then `buildDnatCmds(rule, "A")`, applying the same -C-before-A idempotency pattern as `applyRule` in `manager.go`.
- `removeDnat(rule *DnatRule) error` — runs `buildDnatCmds(rule, "D")`, ignores "not exist" errors via `|| true` in the shell command.

#### 2d. Integration with RestoreAll

In `manager.go`, at the end of `RestoreAll()`, add a call to `m.RestoreAllDnat()`. This ensures DNAT rules are re-applied after container restart in the same sequence as SNAT rules (step 7 in `main.go` startup order is already after `tunnel.Init`).

**Backward compatibility:** `RestoreAllDnat` does a `GetDnatRules()` which returns an empty slice on fresh DB (table exists but empty) — no effect.

---

### Step 3 — API Handlers [Medium — 1.5 h]

**File:** `/Users/jenya/PycharmProjects/cascade/internal/api/nat.go`

Add five new handler functions and register them in `RegisterNat`:

```go
// in RegisterNat():
g.Get("/dnat", getDnatRules)
g.Post("/dnat", createDnatRule)
g.Patch("/dnat/:id", updateDnatRule)
g.Delete("/dnat/:id", deleteDnatRule)
```

Handler signatures (all follow identical patterns to existing outbound NAT handlers):

- `getDnatRules` — calls `nat.Get().GetDnatRules()`, returns `{ rules: [...] }`.
- `createDnatRule` — parses `DnatRuleInput`, calls `AddDnatRule`, returns `201 { rule: {...} }`.
- `updateDnatRule` — toggle shortcut (`len(raw)==1 && "enabled"` key) → `ToggleDnatRule`; otherwise full update → `UpdateDnatRule`. Returns `{ rule: {...} }`.
- `deleteDnatRule` — calls `DeleteDnatRule`, returns `204`.

**CLAUDE.md rule:** both `docs/API.md` and `docs/API.en.md` must be updated in the same commit. See Section 7 of this plan.

---

### Step 4 — Frontend: api.js [Small — 0.5 h]

**File:** `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/js/api.js`

Add four methods after the existing `deleteNatRule` method (around line 721):

```javascript
async getDnatRules() {
  return this.call({ method: 'GET', path: '/nat/dnat' });
},

async createDnatRule(data) {
  return this.call({ method: 'POST', path: '/nat/dnat', body: data });
},

async updateDnatRule({ ruleId, ...updates }) {
  return this.call({ method: 'PATCH', path: `/nat/dnat/${ruleId}`, body: updates });
},

async toggleDnatRule({ ruleId, enabled }) {
  return this.call({ method: 'PATCH', path: `/nat/dnat/${ruleId}`, body: { enabled } });
},

async deleteDnatRule({ ruleId }) {
  return this.call({ method: 'DELETE', path: `/nat/dnat/${ruleId}` });
},
```

Note: all methods use uppercase HTTP verbs (FIX-12 — Node.js 22 llhttp compatibility, still required even in Go/Fiber as the pattern is established in `call()`).

---

### Step 5 — Frontend: app.js — data fields and methods [Medium — 1.5 h]

**File:** `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/js/app.js`

#### 5a. Data fields (insert after `natRuleEdit` block, ~line 357)

```javascript
// Port Forwarding (DNAT)
dnatRules: [],
dnatRulesLoading: false,
showDnatCreate: false,
showDnatEdit: false,
dnatCreate: {
  name: '',
  protocol: 'udp',    // 'tcp' | 'udp' | 'both'
  inPort: '',
  destIp: '',
  destPort: '',       // empty = same as inPort
  comment: '',
},
dnatEdit: {
  id: null,
  name: '',
  protocol: 'udp',
  inPort: '',
  destIp: '',
  destPort: '',
  comment: '',
},
```

#### 5b. Methods (insert into NAT Methods section, ~line 1775)

- `loadDnatRules()` — async, sets `dnatRulesLoading`, calls `api.getDnatRules()`, populates `dnatRules`.
- `openDnatEdit(rule)` — copies rule fields into `dnatEdit`, sets `showDnatEdit = true`.
- `createDnatRule()` — validates inPort non-empty, calls `api.createDnatRule(...)`, resets form, reloads, shows toast.
- `saveDnatRule()` — calls `api.updateDnatRule({ruleId: dnatEdit.id, ...})`, reloads, shows toast.
- `toggleDnatRule(rule)` — calls `api.toggleDnatRule(...)`, reloads.
- `deleteDnatRule(rule)` — confirm dialog, calls `api.deleteDnatRule(...)`, reloads, shows toast.
- `_dnatEffectiveDestPort(rule)` — returns `rule.destPort || rule.inPort` (display helper).

#### 5c. switchNatTab modification

`switchNatTab('portforward')` must also trigger `loadDnatRules()`. Update the existing method at line 1609:

```javascript
switchNatTab(tab) {
  this.activeNatTab = tab;
  if (tab === 'portforward') this.loadDnatRules();
},
```

---

### Step 6 — Frontend: index.html [Medium — 1.5 h]

**File:** `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/index.html`

#### 6a. Enable the Port Forwarding tab button (~line 3197)

Replace the disabled `<button>` with a clickable one:

```html
<button @click="switchNatTab('portforward')"
  :class="activeNatTab === 'portforward' ? 'bg-blue-600 text-white' : 'bg-gray-200 dark:bg-neutral-600 text-gray-700 dark:text-neutral-200'"
  class="px-4 py-2 rounded-lg font-medium transition text-sm">
  Port Forwarding
</button>
```

#### 6b. Replace "Coming soon" placeholder (~lines 3345-3349)

Replace the static Coming soon `<div>` with a full tab content block:

```html
<!-- ── PORT FORWARDING TAB ───────────────────────────────────── -->
<div v-if="activeNatTab === 'portforward'" style="display:flex; flex-direction:column; gap:16px;">

  <!-- Header -->
  <div class="flex items-center justify-between">
    <div>
      <h2 class="text-xl font-semibold dark:text-neutral-200">Port Forwarding Rules</h2>
      <p class="text-sm text-gray-500 dark:text-neutral-400 mt-1">
        DNAT (PREROUTING) — redirect inbound port to a remote host.
      </p>
    </div>
    <button @click="showDnatCreate = true"
      class="px-4 py-2 bg-green-600 text-white rounded text-sm font-medium transition">
      + Add Rule
    </button>
  </div>

  <!-- Loading -->
  <div v-if="dnatRulesLoading" class="text-sm text-gray-400 dark:text-neutral-400" style="padding:16px 0;">
    Loading...
  </div>

  <!-- Empty state -->
  <div v-if="!dnatRulesLoading && dnatRules.length === 0"
    class="text-center text-gray-400 dark:text-neutral-500 rounded-lg bg-gray-50 dark:bg-neutral-800 empty-state">
    No port forwarding rules. Click <strong>+ Add Rule</strong> to create one.
  </div>

  <!-- Rules table -->
  <div v-if="dnatRules.length > 0"
    class="bg-white dark:bg-neutral-800 rounded-lg shadow-sm"
    style="overflow:hidden;">

    <!-- Table header -->
    <div class="flex items-center text-xs font-semibold text-gray-500 dark:text-neutral-400"
      style="padding:8px 16px; border-bottom:1px solid #e5e7eb;">
      <span style="width:16px; flex-shrink:0;"></span>
      <span style="flex:2; padding-left:12px;">Name</span>
      <span style="flex:1;">Protocol</span>
      <span style="flex:1.5;">In Port</span>
      <span style="flex:2.5;">Destination</span>
      <span style="flex:2;" class="text-gray-400 dark:text-neutral-500">Comment</span>
      <span style="width:72px; flex-shrink:0;"></span>
    </div>

    <!-- Rule rows -->
    <div v-for="(rule, idx) in dnatRules" :key="rule.id"
      class="flex items-center"
      :style="idx > 0 ? 'border-top:1px solid #e5e7eb;' : ''"
      style="padding:12px 16px; gap:0;">

      <!-- Enable/disable dot -->
      <span class="shrink-0 cursor-pointer"
        :title="rule.enabled ? 'Disable' : 'Enable'"
        @click="toggleDnatRule(rule)">
        <span v-if="rule.enabled"
          style="display:inline-block;width:12px;height:12px;border-radius:50%;background:#22c55e;box-shadow:0 0 0 2px #bbf7d0;"></span>
        <span v-else
          style="display:inline-block;width:12px;height:12px;border-radius:50%;background:#d1d5db;border:2px solid #9ca3af;"></span>
      </span>

      <!-- Name -->
      <div style="flex:2; padding-left:12px; min-width:0;">
        <div class="font-medium dark:text-neutral-200 truncate">{{ rule.name }}</div>
      </div>

      <!-- Protocol badge -->
      <div style="flex:1; min-width:0;">
        <span class="text-xs rounded-full font-medium"
          style="padding:2px 8px; display:inline-block; background:#dbeafe; color:#1d4ed8;">
          {{ rule.protocol.toUpperCase() }}
        </span>
      </div>

      <!-- In Port -->
      <div style="flex:1.5; min-width:0;">
        <span class="font-mono text-sm dark:text-neutral-300">{{ rule.inPort }}</span>
      </div>

      <!-- Destination -->
      <div style="flex:2.5; min-width:0;">
        <span class="font-mono text-sm dark:text-neutral-300">
          {{ rule.destIp }}:{{ _dnatEffectiveDestPort(rule) }}
        </span>
      </div>

      <!-- Comment -->
      <div style="flex:2; min-width:0;">
        <span v-if="rule.comment" class="text-xs text-gray-400 dark:text-neutral-500 truncate" style="display:block;">
          {{ rule.comment }}
        </span>
        <span v-else class="text-gray-300 dark:text-neutral-600">—</span>
      </div>

      <!-- Actions -->
      <div style="width:72px; flex-shrink:0; display:flex; gap:4px; justify-content:flex-end;">
        <button @click="openDnatEdit(rule)"
          class="text-blue-600 dark:text-blue-400"
          style="background:none; border:none; cursor:pointer; padding:4px 6px; font-size:14px;"
          title="Edit rule">✎</button>
        <button @click="deleteDnatRule(rule)"
          class="text-red-600"
          style="background:none; border:none; cursor:pointer; padding:4px 6px; font-size:16px;"
          title="Delete rule">✕</button>
      </div>
    </div>
  </div>

</div><!-- /portforward tab -->
```

#### 6c. Add Create DNAT Rule modal (after the Edit NAT Rule modal, ~line 3600+)

Pattern: identical structure to "Add NAT Rule" modal. Fields: Name, Protocol (radio: TCP/UDP/Both), Inbound Port (number input), Destination IP (text), Destination Port (number, optional), Comment.

```html
<!-- ── Add DNAT Rule Modal ────────────────────────────────────── -->
<div v-if="showDnatCreate && activePage === 'nat'"
  class="modal-overlay"
  @click.self="showDnatCreate = false">

  <div class="bg-white dark:bg-neutral-700 modal-panel modal-panel-500">
    <div class="modal-header dark:border-neutral-600">
      <h2 class="text-xl font-medium dark:text-neutral-200">Add Port Forwarding Rule</h2>
    </div>
    <div class="modal-body">

      <!-- Name -->
      <div>
        <label class="text-sm font-medium dark:text-neutral-300">Rule Name *</label>
        <input v-model="dnatCreate.name" type="text" placeholder="e.g. Forward WG port to NL"
          class="w-full mt-1 border border-gray-300 dark:bg-neutral-600 dark:border-neutral-500 dark:text-neutral-100 rounded px-3 py-2 text-sm" />
      </div>

      <!-- Protocol -->
      <div>
        <label class="text-sm font-medium dark:text-neutral-300">Protocol *</label>
        <div style="display:flex; gap:16px; margin-top:6px;">
          <label class="flex items-center gap-2 text-sm dark:text-neutral-300 cursor-pointer">
            <input type="radio" v-model="dnatCreate.protocol" value="udp" /> UDP
          </label>
          <label class="flex items-center gap-2 text-sm dark:text-neutral-300 cursor-pointer">
            <input type="radio" v-model="dnatCreate.protocol" value="tcp" /> TCP
          </label>
          <label class="flex items-center gap-2 text-sm dark:text-neutral-300 cursor-pointer">
            <input type="radio" v-model="dnatCreate.protocol" value="both" /> Both
          </label>
        </div>
      </div>

      <!-- Inbound Port -->
      <div>
        <label class="text-sm font-medium dark:text-neutral-300">Inbound Port *</label>
        <input v-model.number="dnatCreate.inPort" type="number" min="1" max="65535" placeholder="e.g. 51820"
          class="w-full mt-1 border border-gray-300 dark:bg-neutral-600 dark:border-neutral-500 dark:text-neutral-100 rounded px-3 py-2 text-sm" />
      </div>

      <!-- Destination IP -->
      <div>
        <label class="text-sm font-medium dark:text-neutral-300">Destination IP *</label>
        <input v-model="dnatCreate.destIp" type="text" placeholder="e.g. 10.100.0.1"
          class="w-full mt-1 border border-gray-300 dark:bg-neutral-600 dark:border-neutral-500 dark:text-neutral-100 rounded px-3 py-2 text-sm" />
      </div>

      <!-- Destination Port (optional) -->
      <div>
        <label class="text-sm font-medium dark:text-neutral-300">Destination Port
          <span class="text-gray-400 font-normal">(optional — defaults to Inbound Port)</span>
        </label>
        <input v-model.number="dnatCreate.destPort" type="number" min="1" max="65535" placeholder="same as inbound"
          class="w-full mt-1 border border-gray-300 dark:bg-neutral-600 dark:border-neutral-500 dark:text-neutral-100 rounded px-3 py-2 text-sm" />
      </div>

      <!-- Comment -->
      <div>
        <label class="text-sm font-medium dark:text-neutral-300">Comment <span class="text-gray-400 font-normal">(optional)</span></label>
        <input v-model="dnatCreate.comment" type="text" placeholder="Optional description"
          class="w-full mt-1 border border-gray-300 dark:bg-neutral-600 dark:border-neutral-500 dark:text-neutral-100 rounded px-3 py-2 text-sm" />
      </div>

    </div>
    <div class="modal-footer-compact dark:border-neutral-600">
      <button @click="showDnatCreate = false"
        class="px-4 py-2 rounded text-sm border border-gray-300 dark:border-neutral-500 dark:text-neutral-300">
        Cancel
      </button>
      <button @click="createDnatRule"
        class="px-4 py-2 bg-green-600 text-white rounded text-sm font-medium">
        Create Rule
      </button>
    </div>
  </div>
</div><!-- /Add DNAT Rule Modal -->
```

#### 6d. Add Edit DNAT Rule modal (same structure as Create, bound to `dnatEdit`)

Identical structure to the Create modal with `v-if="showDnatEdit"`, inputs bound to `dnatEdit.*`, save button calls `saveDnatRule()`.

---

### Step 7 — Documentation [Small — 0.5 h]

**Files** (CLAUDE.md rule: both must be updated in the same commit as the API changes):
- `/Users/jenya/PycharmProjects/cascade/docs/API.md`
- `/Users/jenya/PycharmProjects/cascade/docs/API.en.md`

Add a "Port Forwarding (DNAT)" section under the NAT section in both files documenting the four new endpoints.

---

### Step 8 — Tests [Medium — 1.5 h]

**New file:** `/Users/jenya/PycharmProjects/cascade/internal/nat/dnat_test.go`

Follow the exact pattern from `nat_test.go`. Use `initTestDB(t)` (defined in `nat_test.go`, accessible because same package `nat`).

Test cases to cover:

| Test | What it checks |
|------|----------------|
| `TestValidateDnat_Valid_UDP` | protocol=udp, valid IP and ports |
| `TestValidateDnat_Valid_Both` | protocol=both passes validation |
| `TestValidateDnat_EmptyName` | error when name empty |
| `TestValidateDnat_BadProtocol` | error for protocol="icmp" |
| `TestValidateDnat_ZeroInPort` | error for inPort=0 |
| `TestValidateDnat_PortOutOfRange` | error for inPort=70000 |
| `TestValidateDnat_BadDestIP` | error for destIp="not-an-ip" |
| `TestValidateDnat_BadDestIPCIDR` | error if CIDR is passed instead of bare IP |
| `TestValidateDnat_DestPortZeroAllowed` | destPort=0 is valid (means "same as inPort") |
| `TestBuildDnatCmds_UDP_SamePort` | exact command strings for action A, C, D |
| `TestBuildDnatCmds_UDP_DifferentDestPort` | --to-destination uses destPort |
| `TestBuildDnatCmds_Both_Expands` | protocol=both yields 6 commands (3 × 2 protocols) |
| `TestBuildDnatCmds_DeleteAction` | action D produces -D commands |
| `TestGetDnatRules_EmptyFreshDB` | returns empty slice, no error |
| `TestGetDnatRule_NotFound` | returns nil, no error |
| `TestAddDnatRule_Roundtrip` (DB test, no kernel) | INSERT + SELECT, fields match |
| `TestToggleDnatRule` (DB test) | toggle enabled=false then true |
| `TestDeleteDnatRule` (DB test) | row removed from DB |

Note: tests that call `applyDnat` / `removeDnat` cannot run without a real kernel — those code paths are excluded from unit tests just as `applyRule`/`removeRule` are not tested in `nat_test.go`. DB-touching tests use `initTestDB` which does not call kernel helpers.

---

## 6. Files Modified

| File | Change Type | Complexity |
|------|-------------|------------|
| `/Users/jenya/PycharmProjects/cascade/internal/db/db.go` | Add migration v13 | Small |
| `/Users/jenya/PycharmProjects/cascade/internal/nat/dnat.go` | **New file** — DnatRule types + CRUD methods + iptables helpers | Large |
| `/Users/jenya/PycharmProjects/cascade/internal/nat/manager.go` | Add `RestoreAllDnat()` call at end of `RestoreAll()` | Small |
| `/Users/jenya/PycharmProjects/cascade/internal/api/nat.go` | Add 4 handlers + register routes | Medium |
| `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/js/api.js` | Add 5 DNAT API methods | Small |
| `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/js/app.js` | Add data fields + 7 methods + modify `switchNatTab` | Medium |
| `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/index.html` | Enable tab button, replace placeholder, add 2 modals | Medium |
| `/Users/jenya/PycharmProjects/cascade/internal/nat/dnat_test.go` | **New file** — unit tests | Medium |
| `/Users/jenya/PycharmProjects/cascade/docs/API.md` | Document 4 new endpoints | Small |
| `/Users/jenya/PycharmProjects/cascade/docs/API.en.md` | Document 4 new endpoints (English) | Small |

### Files that must NOT be modified

| File | Reason |
|------|--------|
| `/Users/jenya/PycharmProjects/cascade/cmd/awg-easy/main.go` | `natMgr.RestoreAll()` is already called at step 7 of startup; `RestoreAllDnat` will be invoked from inside `RestoreAll()` — no changes needed in main |
| `/Users/jenya/PycharmProjects/cascade/internal/nat/nat_test.go` | Existing tests must not be touched — only a new `dnat_test.go` is added |
| `src/www/` files | Go rewrite uses `internal/frontend/www/` exclusively (FIX-GO-12) |

---

## 7. Risks and Edge Cases

### Risk 1: Kernel state on container restart
**Issue:** `--network host` means iptables rules survive container restart. If a DNAT rule is deleted from DB while the container is down, `removeDnat` never runs — stale PREROUTING and FORWARD rules remain in the kernel.

**Mitigation:** This is the same risk that exists for SNAT rules (acknowledged in existing code comments). The `-C` before `-A` pattern (`applyDnat`) prevents duplicate rules on restart. For the delete-while-stopped case: document as a known limitation; users can flush manually with `iptables-nft -t nat -F PREROUTING`. A future improvement could be a "flush all DNAT rules then reapply enabled ones" approach in `RestoreAllDnat`.

### Risk 2: `removeDnat` on delete — rule already gone
**Issue:** If the WireGuard interface went down between creation and deletion, iptables may not have the FORWARD rules.

**Mitigation:** In `removeDnat`, use the same pattern as `removeRule`: run the `-D` commands with `|| true` suffix so errors (exit code 1 from iptables) are ignored for the FORWARD rules. The PREROUTING rule failure in `-D` mode should also be tolerated via error-ignore in Go (`log.Printf` but no return error) to allow DB cleanup to proceed.

### Risk 3: `protocol="both"` — port conflict with existing TCP/UDP rule
**Issue:** User creates `protocol=udp inPort=51820` and then `protocol=both inPort=51820`. The `udp` PREROUTING rule already exists; `-C` check for the `udp` subset of `both` will succeed (no duplicate), but the TCP rule will be added. This is correct behaviour — no conflict.

### Risk 4: `destPort=0` JSON serialisation
**Issue:** JavaScript `v-model.number` on an empty input produces `""` (string) not `0`. The backend receives `"destPort": ""` which fails integer parsing.

**Mitigation:** In `createDnatRule()` / `saveDnatRule()` in `app.js`, convert `destPort` before sending:
```javascript
destPort: parseInt(this.dnatCreate.destPort) || 0,
```
Also handle in backend `validate`: accept `destPort=0` as "same as inPort" explicitly (do not error on 0).

### Risk 5: Shell injection in destIP
**Issue:** `destIp` is interpolated into a shell command string.

**Mitigation:** `isIPv4Addr` already rejects anything that is not digits and dots. The validation function is called before any kernel command is built.

### Risk 6: FORWARD chain ordering relative to FirewallManager chains
**Issue:** `FirewallManager` inserts a `FIREWALL_FORWARD` jump at position 1 (start) of FORWARD. DNAT FORWARD rules use `-A FORWARD` (append). This means they land AFTER `FIREWALL_FORWARD`, which is consistent with FIX-1 (use `-A` not `-I` for the same reason as tunnel PostUp).

If a Firewall Rule has action=DROP for the forwarded traffic, that DROP will be evaluated before the DNAT ACCEPT rule. This is intentional — the Firewall Rules take precedence, as they should.

**No code change needed**, but worth documenting so operators know to add a Firewall Rule with ACCEPT action if they have default DROP on FORWARD.

### Risk 7: IPv6
**Scope:** Not in scope. `isIPv4Addr` rejects colons, so an IPv6 `destIp` will fail validation with a clear error. Document this in the UI placeholder text.

---

## 8. Hour Estimate Per File

| File | Hours |
|------|-------|
| `internal/db/db.go` (migration v13) | 0.5 |
| `internal/nat/dnat.go` (new) | 3.5 |
| `internal/nat/manager.go` (RestoreAllDnat hook) | 0.25 |
| `internal/api/nat.go` (4 handlers) | 1.0 |
| `internal/frontend/www/js/api.js` | 0.5 |
| `internal/frontend/www/js/app.js` | 1.0 |
| `internal/frontend/www/index.html` | 1.5 |
| `internal/nat/dnat_test.go` (new) | 1.5 |
| `docs/API.md` + `docs/API.en.md` | 0.5 |
| **Total** | **10.25** |

---

## 9. Implementation Order (recommended)

1. `db.go` migration v13 — unblock everything else.
2. `dnat.go` types + `buildDnatCmds` + validation (no DB yet, write tests first).
3. `dnat_test.go` for pure functions (no DB, no kernel).
4. `dnat.go` CRUD methods using DB (GetDnatRules, GetDnatRule, AddDnatRule, UpdateDnatRule, DeleteDnatRule, ToggleDnatRule, RestoreAllDnat).
5. `dnat_test.go` DB tests (using `initTestDB`).
6. `manager.go` — add `m.RestoreAllDnat()` call in `RestoreAll()`.
7. `api/nat.go` — handlers + registration.
8. `api.js`, `app.js` — data fields + methods.
9. `index.html` — tab activation + placeholder replacement + 2 modals.
10. `docs/API.md` + `docs/API.en.md` — in same commit as step 7.
