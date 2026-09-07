# Issue #105 — stale TCPMSS/MASQUERADE rules on interface update

**Status:** Fixed in [`fix/issue-105-mss-nat-rule-leak-on-update`](https://github.com/JohnnyVBut/cascade/commit/68091ef), merged to `master`.
**Reported by:** [@Locman91](https://github.com/Locman91) — [issue #105](https://github.com/JohnnyVBut/cascade/issues/105)

## Symptom

Changing MSS on an active client interface left the previous PostDown's TCPMSS rule in
`iptables -t mangle -S FORWARD`. Repeating the change (`Auto → Manual 1260 → Manual 1280`)
accumulated multiple simultaneous MSS policies instead of replacing the old one. A plain
interface `Restart` did not clear the accumulated rules either.

## Root cause

`TunnelInterface.Update()` (`internal/tunnel/interface.go`) persisted the new settings and
rewrote the wg-quick config file (`RegenerateConfig()`) **before** calling `Restart()`.
`Restart()` = `Stop()` + `Start()`, and `Stop()`'s `awg-quick down` reads whatever `PostDown`
is *currently on disk* — which, by the time `down` ran, was already the **new** config.

So the `down` step tried to remove a rule keyed by the **new** MSS value/subnet — a rule that
doesn't exist yet, since it hasn't been added by `up` at that point — while the real old rule
(added by the *previous* `PostUp`, keyed by the *old* value) was never touched and leaked
permanently.

The same mechanism affected **NAT**, and arguably worse: toggling `NatDisabled: false → true`
regenerates a config whose `PostDown` no longer contains a MASQUERADE-removal line at all
(the whole NAT block is omitted once `NatDisabled` is true). The old MASQUERADE rule for that
subnet was therefore *never* removed — NAT stayed silently active after being explicitly
disabled in the UI.

Changing the interface `Address` (subnet) hit the same pattern: the old subnet's MASQUERADE
rule leaked whenever the address changed, since the new `PostDown` only knows the new subnet.

### Why only `Update()`

Every other `RegenerateConfig()` call site in the codebase was checked and found not
vulnerable:

- `Create` / `CreateFromConf` / `ImportBackup` (`manager.go`, `import_backup.go`) regenerate
  the config for a **not-yet-started** interface — there are no prior rules to leak.
- `AddPeer` / `UpdatePeer` / `DeletePeer` (`interface.go`) hot-reload peers via
  `KernelSetPeer`/`KernelRemovePeer` (`syncconf`), which never re-executes `PostUp`/`PostDown`
  at all.
- `Start()` regenerates the config immediately before `up`, which only ever executes the
  (already-current) `PostUp` — it never runs a stale `PostDown`.

## Fix

Reordered `Update()`: when a running interface needs a full restart (`addressChanged`,
`listenPortChanged`, `natDisabledChanged`, `mtuChanged`, `mssChanged`, or
`disableRoutesChanged`), the sequence inside the existing background goroutine (behind
`reloadMu`, as before) is now:

1. `Stop()` — **while the on-disk config still reflects the OLD settings** — so `PostDown`
   correctly tears down exactly what the old `PostUp` added.
2. `save()` — persist the new settings.
3. `RegenerateConfig()` — rewrite the config file with the new settings.
4. `Start()` — bring the interface back up, applying the new `PostUp`.

The API call itself is unaffected — `Update()` still returns immediately for this case; it no
longer runs `save()`/`RegenerateConfig()` synchronously before the restart, so any error from
them is now part of the deferred sequence (logged, not returned to the caller) instead of
surfacing on the HTTP response. The not-currently-running branch (nothing to stop, no leak
risk) keeps the previous synchronous `save()` + `RegenerateConfig()` behavior unchanged.

## Tests

### Automated

`internal/tunnel/interface_update_test.go` — covers the behavioral contract change (deferred
vs. synchronous persistence depending on whether the interface is running). Full
`awg-quick`/`iptables` execution isn't exercised in this sandbox/most CI (`RegenerateConfig`
writes to `/etc/amnezia/amneziawg`, not writable without root — same constraint as
`import_conf_psk_test.go`).

```bash
go test ./internal/tunnel/... -race
go test ./...
```

### Manual, on a live server (Ubuntu, kernel/userspace AWG3, real client traffic)

Interface under test: `wg15`, subnet `10.13.34.0/24`, `disableRoutes: false`, `natDisabled: false`.

**MSS** (`PATCH /api/tunnel-interfaces/wg15` with `{"mss": ...}`, checking
`iptables-nft -t mangle -S FORWARD | grep TCPMSS | grep wg15` after each step):

| Step | Result |
|---|---|
| `mss: -1` (Auto) | only `--clamp-mss-to-pmtu` ×2 |
| `mss: 1260` | only `--set-mss 1260` ×2 — clamp rule gone |
| `mss: 1280` | only `--set-mss 1280` ×2 — 1260 rule gone |
| `POST .../restart` | still only `--set-mss 1280` ×2 — no duplicates from Restart |

**NAT** (`PATCH` with `{"natDisabled": ...}`, checking `iptables-nft -t nat -S POSTROUTING | grep 10.13.34`):

| Step | Result |
|---|---|
| baseline | `MASQUERADE` rule present |
| `natDisabled: true` | rule gone |
| `natDisabled: false` | rule present again |

**Address change** (`PATCH` with `{"address": ...}`, checking `iptables-nft -t nat -S POSTROUTING`):

| Step | Result |
|---|---|
| `10.13.34.1/24` → `10.34.13.1/24` | only new subnet's MASQUERADE rule present, old one gone |
| `10.34.13.1/24` → `10.13.34.1/24` (revert) | only original subnet's rule present |

All three scenarios confirmed no rule accumulation on any step, and no leaked/orphaned rules
after Restart or repeated changes.

## Related

- Discovered while investigating this issue: a distinct AWG kernel-module/CLI version-mismatch
  incident on an unrelated server, documented in the "Kernel/CLI version mismatch" and
  "amneziawg-dkms" sections of [README.md](../../README.md#-troubleshooting) — unrelated root
  cause, surfaced during the same debugging session.
