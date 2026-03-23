# Plan: MED-3 — Admin Role + Horizontal Privilege Escalation Fix

## Approach: is_admin flag + ownership check

Admin может менять/удалять любого. Обычный пользователь — только себя.

## Steps

### Step 1 — Migration v9: добавить is_admin (db.go)
```sql
ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;
UPDATE users SET is_admin = 1 WHERE username = 'admin';
-- Fallback: если никто не стал admin (кастомное имя):
UPDATE users SET is_admin = 1 WHERE id = (SELECT id FROM users ORDER BY created_at ASC LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM users WHERE is_admin = 1);
```

### Step 2 — User struct + DB functions (users.go)
- Добавить `IsAdmin bool` в struct User
- Обновить все SELECT (List, GetByID, GetByUsername, VerifyPassword) — добавить is_admin
- Новая функция `IsAdmin(userID string) (bool, error)`
- Новая функция `SetAdmin(userID string, admin bool) error` — с guard: нельзя снять последнего admin
- Расширить `SeedAdminIfEmpty`: новый пользователь → is_admin=1

### Step 3 — callerIsAdmin helper (auth.go)
`callerIsAdmin(c *fiber.Ctx) bool` — вызывает currentUserID → users.IsAdmin.
Open mode (0 users) → вернуть true.

### Step 4 — Guards + новый handler (users.go)
- `listUsers` → только admin (403 иначе)
- `createUser` → только admin (403 иначе)
- `updateUser` → `callerID != id && !callerIsAdmin` → 403
- `deleteUser` → `callerID != id && !callerIsAdmin` → 403
- Новый `setAdmin (POST /api/users/:id/set-admin)` → только admin

## Files to change
| File | Change |
|------|--------|
| `internal/db/db.go` | Migration v9 |
| `internal/users/users.go` | IsAdmin field + IsAdmin/SetAdmin/CountAdmins functions |
| `internal/api/auth.go` | callerIsAdmin() helper |
| `internal/api/users.go` | Guards + setAdmin handler |

## NOT changing
- `cmd/awg-easy/main.go`
- `internal/tokens/`
- Frontend

## API contract after fix
```
GET    /api/users              — admin only
POST   /api/users              — admin only
GET    /api/users/me           — current user
PATCH  /api/users/me           — current user (own password)
PATCH  /api/users/:id          — admin OR owner
DELETE /api/users/:id          — admin OR owner
POST   /api/users/:id/set-admin — admin only  [NEW]
```

## Edge cases
- Единственный admin снимает себе is_admin → 400 "cannot remove last admin"
- Инсталляция с кастомным username → fallback migration выставит is_admin=1 первому по created_at
- Open mode (0 users) → callerIsAdmin=true, всё проходит
