# Plan: HIGH-3 — PrivateKey injection into wg-quick config via /restore

## Vulnerability

`PUT /api/tunnel-interfaces/:id/restore` → `AddPeer()` → `fmt.Sprintf("PrivateKey = %s\n", p.PrivateKey)`
Newline в PrivateKey → inject PostUp → RCE при запуске интерфейса.

## Fix location: AddPeer() в interface.go

Все пути создания пиров сходятся здесь: create, import-json, restore.
НЕ патчить только handler — это anti-pattern.

## Fields to validate (все идут в .conf файл)

| Field | Validator | Guard |
|---|---|---|
| PrivateKey | validate.WGKey | if != "" (interconnect peers имеют пустой) |
| ClientAllowedIPs | validate.CIDR | if != "" |
| Address | validate.CIDR | if != "" |

WGKey() уже покрывает PrivateKey (тот же формат: 32-byte Curve25519, base64).

## Files to change

| File | Change |
|---|---|
| `internal/tunnel/interface.go` | +3 валидационных guard в AddPeer() |
| `internal/validate/validate_test.go` | WGKey тесты: newline injection, empty string |

## NOT changing

- `internal/api/interfaces.go` — handler правильный
- `internal/peer/peer.go` — DB integrity, не injection prevention
- `internal/validate/validate.go` — WGKey уже есть

## Edge cases

- PrivateKey="" → пропустить (interconnect peers)
- ClientAllowedIPs="" → пропустить (default применится в CreatePeer)
- Address="10.8.0.2/24" → net.ParseCIDR принимает host bits, validate.CIDR тоже
