# v0.9.5

## 🚀 Headline: AmneziaWG 3.0 support

Cascade now supports **AmneziaWG 3.0** as a full third protocol, alongside plain WireGuard and AWG 2.x.

- New Transport Protection fields (S3/S4) are generated, persisted, and rendered into `wg-quick` configs and client `.conf` files
- `RandomTrailers` / `DisableCookies` are real, user-controllable toggles (for 3.0), plus static fields for 3.0
- `PersistentKeepalive` range support for AWG 3.1+, with an explicit template version
- AWG3 fields carried through template export/import and supported in the Site-to-Site (S2S) wizard
- Legacy frontend updated to support AWG 3.0
- Warning when the AWG CLI and kernel-module versions don't match

## 🛠 Fixes

- Fixed a race in the in-memory peer cache — now keyed by `Peer.ID` instead of the caller-supplied ID
- Closed two races in one-time-link (OTL) config download
- Fixed a crash that hid obfuscation params when manually creating a v3 peer
- Fixed the protocol label on the interface detail card
- Third-party WireGuard uplinks no longer get an auto-generated `PresharedKey`
- Closed a validation gap: client `.conf` files with Transport Protection fields now parse correctly

## 📦 Deployment

- `backup.sh` — safe data backup before upgrading
- `switch-mode.sh` now picks up `docker-compose.override.yml`
- Mounted `/lib/modules` so kernel-module version detection works correctly
- Added `kmod` to the image so `modinfo` can read compressed kernel modules
- Kernel headers are now installed before the DKMS build in kernel-module mode
- Isolated/OVS compose no longer forces userspace mode

## 📚 Documentation

- Noted git as a prerequisite for cloning the repo
- Added disk-cleanup troubleshooting steps for small VPS installs
- Warned kernel-module users to re-sync the kernel module after updating

---

**Full changelog:** `v0.9.4...v0.9.5`
