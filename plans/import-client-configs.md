# Plan: Import Client WireGuard Configs

## Goal

Allow users to upload multiple client `.conf` files so that the private key stored
in each file is saved back to the matching peer on the server (matched by public key
derivation). Once saved, `downloadableConfig = true` enables QR and config-download
buttons for those peers.

---

## Research Findings

### Key existing mechanisms

| Item | Location | Detail |
|------|----------|--------|
| `Peer.PrivateKey` | `internal/peer/peer.go:46` | Persisted in SQLite; empty for interconnect/manually-added peers |
| `Peer.DownloadableConfig` | `peer.go:73` | Computed field: `PrivateKey != ""` |
| `updatePeer()` | `peer.go:768` | Does NOT touch `private_key` column — dedicated SQL needed |
| `DerivePublicKey(bin, priv)` | `peer.go:440` | Shells out `echo <priv> | wg pubkey` — already used in manager.go |
| `ParseWGConf(content)` | `tunnel/conf_parser.go:77` | Parses full `.conf`; returns `ParsedConf{PrivateKey, Address, ...}` |
| `c.FormFile("backup")` | `api/system.go:212` | Pattern for single-file multipart upload in Fiber |
| Protocol-aware binary | `manager.go:199` | `syncBin = "wg"` or `"awg"` based on interface `Protocol` field |
| `sanitizePeer()` | `api/peers.go:109` | Strips PrivateKey from JSON responses — must be used in response |
| `disableRoutes` flag | `TunnelInterface` | False = client interface (has downloadable configs); true = S2S transit |

### What does NOT exist yet

- No endpoint for bulk private-key injection
- No `SavePrivateKey` / `UpdatePrivateKey` DB function in the peer package
- No multipart multi-file upload pattern in peer handlers (only single-file in system.go)
- No UI flow for "import client configs" on a per-interface basis

### Risks and gotchas

1. **`wg pubkey` subprocess per file** — each derivation shells out. For 500 files
   this is slow but acceptable (< 1 s each). If performance becomes an issue,
   pure-Go `golang.org/x/crypto/curve25519` can replace it (already a dependency).
2. **Private key validation** — `ParseWGConf` requires `PrivateKey != ""` and the key
   to be a valid WG base-64 scalar. The existing `validate.WGKey` helper can be reused.
3. **Wrong interface** — a `.conf` whose public-key does not match any peer on this
   interface must be tracked as "unmatched" and reported. Never silently skip.
4. **Duplicate upload** — uploading the same file twice is idempotent: UPDATE sets the
   same value. No special handling needed.
5. **Tailwind CSS** — only pre-compiled classes in `app.css` may be used. Any new
   layout in index.html must use inline `style="..."` for spacing not already present.
6. **Vue 2 array mutation** — after API response, update `tunnelInterfaces` via
   `this.tunnelInterfaces.splice(idx, 1, updatedIface)`.
7. **Protocol binary** — must look up the interface's `protocol` field to pick `"wg"`
   vs `"awg"` when calling `DerivePublicKey`.
8. **`sanitizePeer` in response** — the handler must never return `PrivateKey` in JSON.

---

## Files to Modify

| File | Change | Complexity |
|------|--------|------------|
| `internal/peer/peer.go` | Add `SavePrivateKey(peerID, privKey string) error` | Small |
| `internal/api/peers.go` | Add `importClientConfigs` handler; register route | Medium |
| `internal/frontend/www/js/api.js` | Add `importClientConfigs({ interfaceId, files })` | Small |
| `internal/frontend/www/js/app.js` | Add data properties + handler methods | Small |
| `internal/frontend/www/index.html` | Add button in peer-list toolbar + result modal | Medium |

### Files that must NOT be modified

- `internal/tunnel/conf_parser.go` — already does everything needed; no changes required
- `internal/db/db.go` — no schema change; `private_key` column exists since migration v1
- `internal/tunnel/manager.go` — no new tunnel-level method needed for this feature

---

## Step-by-Step Implementation Plan

### Step 1 — `peer.SavePrivateKey` (small)

**File:** `internal/peer/peer.go`

Add after `SaveHandshake`:

```go
// SavePrivateKey persists a private key for a peer and updates DownloadableConfig.
// Called by the import-client-configs handler after verifying the derived public key
// matches the peer's stored public key.
// UPDATE on a non-existent peer is a no-op — safe after peer deletion.
func SavePrivateKey(peerID, privateKey string) error {
    _, err := db.DB().Exec(
        `UPDATE peers SET private_key = ? WHERE id = ?`,
        privateKey, peerID,
    )
    return err
}
```

No migration needed — column already exists.

---

### Step 2 — Backend handler (medium)

**File:** `internal/api/peers.go`

#### 2a. Register route

In `RegisterPeers`, add before the `/:peerId` group (like `import-json`):

```go
g.Post("/import-client-configs", importClientConfigs)
```

#### 2b. Handler signature

```go
// POST /api/tunnel-interfaces/:id/peers/import-client-configs
// Accepts multipart form; field name "configs[]" may contain multiple .conf files.
// For each file: parses PrivateKey from [Interface], derives public key,
// finds matching peer in this interface, saves private key.
// Returns: { matched: N, unmatched: ["file1.conf", ...], peers: [...sanitized peers] }
func importClientConfigs(c *fiber.Ctx) error
```

#### 2c. Handler logic (pseudo-code, not real code)

```
ifaceID = c.Params("id")
iface   = mgr().GetInterface(ifaceID)   // need to expose or inline
if iface == nil → 404

form, err = c.MultipartForm()
files = form.File["configs[]"]
if len(files) == 0 → 400 "no files uploaded"

// Build lookup: publicKey → *peer.Peer for this interface
peerByPubKey = map[string]*peer.Peer{}
for each peer in iface.GetAllPeers():
    peerByPubKey[peer.PublicKey] = peer

// Determine wg/awg binary from interface protocol
syncBin = "wg"
if iface.Protocol == "amneziawg-2.0":
    syncBin = "awg"

matched   = 0
unmatched = []string{}
updated   = []*peer.Peer{}

for each fileHeader in files:
    open file
    read content
    parsed, err = tunnel.ParseWGConf(content)
    if err → add filename to unmatched, continue

    derivedPub, err = peer.DerivePublicKey(syncBin, parsed.PrivateKey)
    if err → add filename to unmatched, continue

    p, ok = peerByPubKey[derivedPub]
    if !ok → add filename to unmatched, continue

    err = peer.SavePrivateKey(p.ID, parsed.PrivateKey)
    if err → log error, add filename to unmatched, continue

    // Reload peer to get updated DownloadableConfig
    refreshed, err = peer.GetPeer(p.ID)
    if err == nil && refreshed != nil:
        iface.ReloadPeer(refreshed)   // update in-memory (see note below)
        updated = append(updated, refreshed)
    matched++

return c.JSON(fiber.Map{
    "matched":   matched,
    "unmatched": unmatched,
    "peers":     sanitizePeers(updated),
})
```

**Note on in-memory reload:** `TunnelInterface` keeps peers in memory (polled each second).
After saving to SQLite, the in-memory copy will not have `DownloadableConfig = true`
until the next load. The simplest approach: call `iface.ReloadPeers()` if such a method
exists, OR just return the updated peers from SQLite and let the frontend re-fetch via
`loadTunnelInterfaces()`. The frontend already calls `loadTunnelInterfaces()` after the
API call, so in-memory staleness is fine for ≤ 1 second. No new tunnel method is required.

---

### Step 3 — Frontend API method (small)

**File:** `internal/frontend/www/js/api.js`

Add near `importPeerJSON` (around line 593):

```js
/**
 * Upload multiple client .conf files to import private keys into existing peers.
 * @param {{ interfaceId: string, files: FileList|File[] }}
 * @returns {{ matched: number, unmatched: string[], peers: object[] }}
 */
async importClientConfigs({ interfaceId, files }) {
  const form = new FormData();
  for (const file of files) {
    form.append('configs[]', file);
  }
  const res = await fetch(`${this._apiBase()}/tunnel-interfaces/${interfaceId}/peers/import-client-configs`, {
    method: 'POST',
    headers: this._authHeaders(),   // check existing pattern — Bearer token header
    body: form,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }));
    throw new Error(err.message || res.statusText);
  }
  return res.json();
}
```

**Note:** Verify the exact `_authHeaders()` / `_apiBase()` method names — look at how
`previewSystemRestore` calls `fetch` vs how `call()` works. The multipart form cannot go
through `call()` (which sets `Content-Type: application/json`). Use raw `fetch` like
`restoreSystemBackup` does.

---

### Step 4 — Frontend data properties + methods (small)

**File:** `internal/frontend/www/js/app.js`

#### 4a. New data properties (add to `data()` near line 242)

```js
showImportClientConfigs: false,
importClientConfigsResult: null,  // { matched, unmatched, peers }
```

#### 4b. New methods

```js
// Trigger hidden file input for client config import
openImportClientConfigs() {
  this.$refs.importClientConfigsInput.click();
},

async onImportClientConfigsSelected(event) {
  const files = event.target.files;
  if (!files || files.length === 0) return;
  const ifaceId = this.activeInterfaceId;
  if (!ifaceId) return;

  try {
    const res = await this.api.importClientConfigs({ interfaceId: ifaceId, files });
    this.importClientConfigsResult = res;
    this.showImportClientConfigs = true;
    // Reload to update downloadableConfig on all peers
    await this.loadTunnelInterfaces();
    // Reset file input so same selection can be re-triggered
    event.target.value = '';
  } catch (err) {
    this.showToast(`Import failed: ${err.message}`, 'error');
    event.target.value = '';
  }
},
```

---

### Step 5 — UI: button in peer-list toolbar (medium)

**File:** `internal/frontend/www/index.html`

#### 5a. Hidden file input + "Import Configs" button

Location: In the peer-list toolbar (around line 2396), after the "Restore" label and
before "Manual" button. Only show when `!currentInterface.disableRoutes` (client
interface, not S2S transit).

```html
<!-- Import client configs: inject private keys from .conf files -->
<label v-if="currentInterface && !currentInterface.disableRoutes"
  title="Import private keys from client .conf files — enables QR and config download"
  class="hover:cursor-pointer hover:bg-red-800 hover:border-red-800 hover:text-white text-gray-700 dark:text-neutral-200 max-md:border-x-0 border-2 border-gray-100 dark:border-neutral-600 py-2 px-4 md:rounded inline-flex items-center transition">
  <svg class="w-4 md:mr-2" xmlns="http://www.w3.org/2000/svg" fill="none"
    viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
    <path stroke-linecap="round" stroke-linejoin="round"
      d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5m-13.5-9L12 3m0 0 4.5 4.5M12 3v13.5"/>
  </svg>
  <span class="max-md:hidden text-sm">Import Configs</span>
  <input type="file" accept=".conf" multiple
    @change="onImportClientConfigsSelected"
    ref="importClientConfigsInput"
    class="hidden"/>
</label>
```

**Tailwind check needed before implementation:**
- `hover:bg-red-800`, `hover:border-red-800`, `hover:text-white` — already used on Backup/Restore buttons on same line, so they exist in app.css.
- `max-md:border-x-0`, `border-2`, `border-gray-100`, `dark:border-neutral-600`, `py-2`, `px-4`, `md:rounded`, `inline-flex`, `items-center`, `transition` — all used on adjacent buttons, safe.

#### 5b. Result modal

Location: After the peer-list section or alongside other modals (search for `v-if="showImportBackup"` and add below).

```html
<!-- Import Client Configs Result Modal -->
<div v-if="showImportClientConfigs"
  class="fixed inset-0 bg-black bg-opacity-50 z-50 overflow-y-auto"
  style="padding: 40px 24px;"
  @click.self="showImportClientConfigs = false">
  <div class="bg-white dark:bg-neutral-700 rounded-lg" style="max-width: 480px; margin: 0 auto;">
    <div class="p-4 border-b border-gray-200 dark:border-neutral-600 flex items-center justify-between">
      <h3 class="text-lg font-semibold dark:text-neutral-200">Import Client Configs</h3>
      <button @click="showImportClientConfigs = false"
        class="text-gray-400 hover:text-gray-600 dark:hover:text-neutral-200">
        <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
        </svg>
      </button>
    </div>
    <div class="p-4" v-if="importClientConfigsResult">
      <p class="dark:text-neutral-200" style="margin-bottom: 12px;">
        <strong>{{ importClientConfigsResult.matched }}</strong> peer(s) updated successfully.
      </p>
      <div v-if="importClientConfigsResult.unmatched && importClientConfigsResult.unmatched.length > 0">
        <p class="text-sm text-red-500" style="margin-bottom: 6px;">
          {{ importClientConfigsResult.unmatched.length }} file(s) not matched:
        </p>
        <ul style="margin-left: 16px; list-style: disc;">
          <li v-for="f in importClientConfigsResult.unmatched" :key="f"
            class="text-sm dark:text-neutral-300">{{ f }}</li>
        </ul>
      </div>
      <p v-else class="text-sm text-green-600">All files matched.</p>
    </div>
    <div class="p-4" style="text-align: right; border-top: 1px solid #e5e7eb;">
      <button @click="showImportClientConfigs = false"
        class="px-4 py-2 text-sm bg-blue-600 text-white rounded hover:bg-blue-700">
        OK
      </button>
    </div>
  </div>
</div>
```

**Tailwind check needed before implementation:**
- `fixed inset-0 bg-black bg-opacity-50 z-50 overflow-y-auto` — standard modal pattern from CLAUDE.md, already in app.css.
- `p-4`, `border-b`, `border-gray-200`, `dark:border-neutral-600`, `text-lg`, `font-semibold`, `dark:text-neutral-200`, `w-5`, `h-5`, `text-sm`, `text-red-500`, `text-green-600`, `dark:text-neutral-300` — verify each with grep before use.
- `bg-blue-600`, `text-white`, `rounded`, `hover:bg-blue-700` — verify before use; use inline style if absent.

---

### Step 6 (optional) — Post-import-backup follow-up prompt (small)

**File:** `internal/frontend/www/js/app.js` — inside `doImportBackup()` success path (around line 1483)

After the interface is imported and started, offer to import client configs:

```js
// After successful import backup, check if this is a client interface
const iface = res.interface || {};
if (!iface.disableRoutes && res.peersCreated > 0) {
  // Prompt user to import client configs
  this.activeInterfaceId = iface.id;
  this.showToast(
    `${iface.id} imported. Use "Import Configs" button to restore client private keys.`,
    'info'
  );
}
```

This is non-intrusive — just a toast hint. A more elaborate "follow-up wizard" can be
added later without touching the backend.

---

## Complexity Summary

| Step | File(s) | Complexity |
|------|---------|------------|
| 1. `SavePrivateKey` in peer.go | `peer/peer.go` | Small |
| 2. `importClientConfigs` handler + route | `api/peers.go` | Medium |
| 3. `importClientConfigs` in api.js | `js/api.js` | Small |
| 4. Data props + methods in app.js | `js/app.js` | Small |
| 5a. Button in peer toolbar | `index.html` | Small |
| 5b. Result modal | `index.html` | Small |
| 6. Post-import-backup hint | `js/app.js` | Small |

**Total estimated effort: 1–2 hours of focused implementation.**

---

## Edge Cases and Risks

| Risk | Mitigation |
|------|-----------|
| File with no `[Interface]` / missing `PrivateKey` | `ParseWGConf` returns error → filename added to `unmatched` |
| Derived public key does not match any peer | Filename added to `unmatched`, no DB write |
| AWG interface: `wg pubkey` won't parse AWG key | Use `awg pubkey` (syncBin logic from manager.go) |
| Very large batch (1000+ files) | Accept as-is; each derivation < 5ms; 1000 files ≈ 5s. Acceptable |
| User uploads server .conf instead of client .conf | `ParseWGConf` will succeed (has PrivateKey); derived key will likely not match any peer → `unmatched`. Safe |
| Multipart body size limit | Fiber default is 4MB. Each `.conf` is < 1KB; 1000 files = ~1MB. Fine |
| Race condition: peer deleted between lookup and SavePrivateKey | `SavePrivateKey` is a no-op for non-existent IDs. Safe |

---

## Backward Compatibility

- No schema changes. The `private_key` column exists since migration v1.
- No changes to existing endpoints.
- `downloadableConfig` field is already in the peer JSON response; front-end already
  gates QR/config buttons on it. No UI changes needed for existing functionality.
- The new button only appears for client interfaces (`!disableRoutes`). S2S/transit
  interfaces are unaffected.
