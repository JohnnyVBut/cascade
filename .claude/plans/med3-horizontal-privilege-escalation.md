# Plan: MED-3 — Horizontal Privilege Escalation in /api/users/:id

## Vulnerability

`PATCH /api/users/:id` и `DELETE /api/users/:id` не проверяют что caller == owner.
Любой авторизованный пользователь может менять пароль/username или удалять любого другого.

## Approach: ownership check (self-only)

Схема не имеет `is_admin` колонки — RBAC отсутствует. Минимальный правильный фикс:
caller может менять/удалять только свой аккаунт (`callerID == targetID`).
Иначе → 403 Forbidden.

## How to get current user ID

`currentUserID(c)` в `internal/api/auth.go:367` — уже существует, обрабатывает
session cookie и Bearer token. Просто не вызывается в updateUser/deleteUser.

## Edge cases

| Case | Result |
|------|--------|
| User updates/deletes own account | OK |
| User tries to change another | 403 |
| Last user tries to delete self | 400 (существующий guard в users.Delete) |
| Bearer token auth | OK — currentUserID читает token owner |

## Files to change

| File | Change |
|------|--------|
| `internal/api/users.go` | +ownership guard в updateUser и deleteUser |

## NOT changing

- `internal/users/users.go` — DB layer корректен
- `internal/api/auth.go` — currentUserID корректен
- `internal/db/db.go` — schema migration не нужна
- `cmd/awg-easy/main.go` — routes без изменений

## Implementation

В `updateUser` и `deleteUser` — в начале, до парсинга body:
1. `callerID, ok := currentUserID(c)` — если !ok → 401
2. если `callerID != id` → 403 Forbidden
3. продолжать существующую логику

## Future

При добавлении RBAC (wishlist) — добавить `|| callerIsAdmin(c)` к проверке.
