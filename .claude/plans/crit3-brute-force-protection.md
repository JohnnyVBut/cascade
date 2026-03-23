# Plan: CRIT-3 — Brute-force protection on POST /api/session

## Problem

`POST /api/session` has no rate limiting, no delay on failure, no lockout.
Словарная атака: ~1 час. Таргетированная: минуты.

**Найдено:** SECURITY.md заявляет "Rate limit: 5 POST/min per IP" — но в Caddyfile
этой директивы нет. Caddy community edition не имеет встроенного rate limiting.

## Approach

### Level 1 (required): Fiber limiter middleware
- Package: `github.com/gofiber/fiber/v2/middleware/limiter` — уже в go.mod, нулевые зависимости
- Params: 5 attempts / 1 minute / IP
- Response: 429 + Retry-After: 60
- Scope: только `POST /api/session` (не глобальный)

### Level 2 (recommended): Artificial delay on failure
- `time.Sleep(500ms)` в handler после неудачного `VerifyPassword`
- Суммарно с bcrypt cost 12: ~750ms на неудачную попытку
- Не блокирует горутину надолго, rate limiter снижает параллелизм

### NOT implementing
- Account lockout — риск самозапирания единственного admin
- Redis — избыточно
- CAPTCHA — несовместима с self-hosted

## Files to change

| File | Change |
|------|--------|
| `internal/api/auth.go` | time.Sleep(500ms) при неудачном логине |
| `cmd/awg-easy/main.go` | limiter middleware на login route |

## Tests

- `go test ./internal/api/...` после изменений
- Проверить что limiter возвращает 429 после 5 неудач
- Проверить что успешный логин не замедляется
