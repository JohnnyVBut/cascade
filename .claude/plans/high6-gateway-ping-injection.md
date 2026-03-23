# Plan: HIGH-6 — Command Injection via ping in gateway/monitor.go

## Vulnerable line (monitor.go ~201)

```go
cmd := fmt.Sprintf("ping -c 1 -W 1 -I %s %s", gw.Interface, target)
```

`gw.Interface` и `target` (= `gw.MonitorAddress` или `gw.GatewayIP`) — user input,
хранятся в SQLite без валидации, подставляются в `bash -c`.

## Steps

### Step 1 — validate.HostOrIP (validate.go)
Новая функция для MonitorAddress (может быть IP или hostname):
- `""` → nil (поле опционально)
- `net.ParseIP(s) != nil` → nil
- Иначе: RFC 1123 hostname validation + проверка shell metacharacters

### Step 2 — Тесты для HostOrIP (validate_test.go)

### Step 3 — validateGatewayInput (gateway/manager.go)
- `validate.IfaceName(inp.Interface)`
- `validate.IP(inp.GatewayIP)`
- `validate.HostOrIP(inp.MonitorAddress)`

### Step 4 — Defence-in-depth в probeICMP (gateway/monitor.go)
Перед fmt.Sprintf: validate.IfaceName + validate.HostOrIP → при ошибке log + return.
Защищает от legacy строк в БД.

## Files to change
| File | Change |
|------|--------|
| `internal/validate/validate.go` | Add HostOrIP() |
| `internal/validate/validate_test.go` | Add TestHostOrIP_* |
| `internal/gateway/manager.go` | Tighten validateGatewayInput |
| `internal/gateway/monitor.go` | Defence-in-depth guard in probeICMP |

## NOT changing
- `internal/firewall/manager.go` — gateway fields safe by construction after Step 3
- `internal/api/gateways.go` — валидация в manager, не в handler

## Key edge cases
- MonitorAddress может быть hostname (`google.com`) → нужен HostOrIP, не только IP
- GatewayIP — только IP (используется в `ip route replace via`)
- Legacy rows в БД — defence-in-depth guard пропускает probe молча
- IPv6 в MonitorAddress — net.ParseIP обрабатывает корректно
