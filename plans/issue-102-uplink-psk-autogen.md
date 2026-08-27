# Issue #102 — Uplink .conf import auto-adds PresharedKey, breaks handshake

https://github.com/JohnnyVBut/cascade/issues/102

## Summary

Importing a third-party WireGuard `.conf` (e.g. Cloudflare WARP, generated
via `wgcf`) as an Uplink (S2S) interface silently adds a `PresharedKey` that
was never present in the source config. The remote server has no knowledge
of this locally-invented PSK, so the handshake fails. Removing the PSK
manually (`wg set <iface> peer <pubkey> preshared-key /dev/null`) fixes the
handshake immediately — but Cascade re-adds the PSK on every interface
restart, since it's persisted in the peer record and re-applied from config
on every `Start()`.

Reported by @Locman91, reproduced against a WARP `.conf` with no
`PresharedKey` line at all.

## Root cause

`TunnelInterface.AddPeer()` (`internal/tunnel/interface.go:525-535`):

```go
// Interconnect peers always need a PSK for mutual authentication.
// If none was provided (first importer in S2S workflow), generate one now.
// The importer exports their params with this PSK so the remote side can
// import it and the two ends end up with a matching PSK.
if inp.PeerType == "interconnect" && inp.PresharedKey == "" {
    psk, err := peer.GeneratePSK(t.syncBin())
    if err != nil {
        return nil, fmt.Errorf("generate PSK for interconnect peer: %w", err)
    }
    inp.PresharedKey = psk
}
```

This unconditional auto-generation is correct for exactly **one** of the two
call sites that create `PeerType: "interconnect"` peers, and wrong for the
other:

1. **`importPeerJSON`** (`internal/api/peers.go:210-264`, `POST
   /api/tunnel-interfaces/:id/peers/import-json`) — Cascade↔Cascade S2S
   interconnect. The first side to import a remote's exported params (which
   don't yet include a PSK) needs to invent one; it then exports its own
   params back (`exportPeerParams`, `internal/tunnel/interface.go:1541-1542`
   forwards `p.PresharedKey` when set), so the second Cascade instance
   imports that same PSK and both ends match. Auto-generation here is the
   intended mechanism, not a bug — already noted in a comment at
   `internal/api/peers.go:256-257`.

2. **`ImportConf`** (`internal/tunnel/manager.go:396-444`, the Uplink `.conf`
   import used by the frontend's "Interfaces → Import .conf → Uplink (S2S)"
   flow) — imports a `.conf` from an arbitrary WireGuard server, which may or
   may not be another Cascade instance. It builds the peer with:
   ```go
   p, err := iface.AddPeer(peer.PeerInput{
       Name:         "upstream",
       PublicKey:    parsed.PeerPublicKey,
       PresharedKey: parsed.PeerPresharedKey, // "" when the .conf has no PresharedKey line
       ...
       PeerType: "interconnect",
       ...
   })
   ```
   `parsed.PeerPresharedKey` is `""` whenever the source `.conf`'s `[Peer]`
   section has no `PresharedKey` line (`internal/tunnel/conf_parser.go`'s
   `case "presharedkey":` only sets it if the key is present). Because this
   also goes through `AddPeer` with `PeerType: "interconnect"` and an empty
   `PresharedKey`, the exact same auto-generation fires — but there is no
   round-trip here. The remote server (WARP, or any other third-party WG
   peer) never learns this PSK, so every handshake using it fails.

The two call sites share `PeerType: "interconnect"` as a matching label for
unrelated reasons — one means "the other end is another Cascade instance we
control", the other means "the other end is some external upstream we're
connecting to" — and `AddPeer`'s PSK logic conflates them.

Confirmed by direct reproduction of the reporter's steps against the current
`ImportConf`/`AddPeer` code path; no separate live testing needed beyond
tracing the exact data flow from `conf_parser.go` → `manager.go` →
`interface.go`.

## Proposed fix

Additive, minimal-surface change — do not alter the S2S JSON-import
behavior at all, only stop `ImportConf` from silently opting into it.

1. Add an internal-only flag to `peer.PeerInput`
   (`internal/peer/peer.go:102-120`):
   ```go
   // Special flags (not stored directly)
   GenerateKeys    bool `json:"generateKeys"`
   AutoAllocateIP  bool `json:"autoAllocateIP"`
   AutoGeneratePSK bool `json:"-"` // interconnect peers only — see AddPeer's PSK logic
   ```
   `json:"-"`: this is a server-internal decision made by the specific
   caller (`importPeerJSON`), not something a client should be able to
   request via the public peer-creation API body.

2. Change `AddPeer`'s condition (`internal/tunnel/interface.go:529`):
   ```go
   if inp.PeerType == "interconnect" && inp.PresharedKey == "" && inp.AutoGeneratePSK {
   ```

3. `importPeerJSON` (`internal/api/peers.go`) sets the flag explicitly when
   building `PeerInput`:
   ```go
   inp := peer.PeerInput{
       PeerType:        "interconnect",
       AutoGeneratePSK: true,
   }
   ```
   (Replaces the now-inaccurate comment at lines 256-257 explaining the
   "automatic" behavior — comment should instead say generation is opt-in
   via this flag.)

4. `ImportConf` (`internal/tunnel/manager.go`) — **no code change**. It
   already only sets `PresharedKey: parsed.PeerPresharedKey`, never touches
   the new flag, so it defaults to `false`. Result: if the source `.conf`
   has a `PresharedKey`, it's used as-is (already correct today); if not,
   the peer is created with no PSK — exactly the behavior requested in the
   issue ("если PresharedKey в конфиге нет — не генерировать его
   автоматически").

### Why not gate on something else (e.g. a new PeerType, or checking Endpoint)

- A third `PeerType` (e.g. `"uplink"` vs `"interconnect"`) would be a larger,
  more invasive change touching every place `PeerType == "interconnect"` is
  checked today (export/import JSON, `HasAWG3Fields`-style validation,
  frontend peer-type badges, etc.) for no behavioral gain over a single
  opt-in bool scoped to exactly the one decision that's actually ambiguous.
- Inferring intent from other fields (e.g. "has `ClientAllowedIPs` ==
  `AllowedIPs`") would be fragile and implicit — the explicit flag makes the
  two call sites' differing intent obvious at the call site itself, matching
  this codebase's established preference for explicit fields over inferred
  ones (see the AWG3 template-version-inference bug fixed earlier this
  session — the exact same class of mistake, implicit inference from
  unrelated data instead of an explicit signal).

## Regression risk / what needs testing

- Existing S2S Cascade↔Cascade flow (`importPeerJSON`) must be unaffected —
  regression test: import a JSON export with no `presharedKey` field, assert
  the created peer still gets an auto-generated PSK.
- `ImportConf` with a `.conf` that HAS a `PresharedKey` line must still use
  it as-is (already works, but add an explicit regression test — this is
  the "should keep working" half of the fix).
- `ImportConf` with a `.conf` that has NO `PresharedKey` line must now
  create the peer with an empty `PresharedKey` (the actual bug fix) —
  regression test asserting `p.PresharedKey == ""` after import.
- Check whether `TunnelInterface.Start()`/config regeneration path
  (`ToWgConfig` in `internal/peer/peer.go`) already correctly omits the
  `PresharedKey =` line when empty (it should — `if p.PresharedKey != ""`
  guards it already, per existing code read this session), so no separate
  fix needed there — just confirm with a test.
