# Plan: Multi-Server Management

**Feature:** Управление удалёнными Cascade-серверами через единый UI.  
**Complexity:** Large  
**DB migration:** v30

---

## Overview архитектуры

```
Browser → Current Cascade (session auth) → /api/remotes/:id/proxy/* → Remote Cascade (Bearer token)
```

Никакого прямого Browser → Remote. Пароль не хранится. Токен храниться plain-text в SQLite (достаточно — он уже ограничен по времени и отзываем на remote).

---

## Шаг 1 — DB Migration v30 (small)

**Файл:** `internal/db/db.go`

Добавить миграцию в конец списка `migrations`:

```sql
-- v30: remote Cascade servers for multi-server management.
CREATE TABLE IF NOT EXISTS remotes (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    url        TEXT NOT NULL DEFAULT '',   -- e.g. "https://vpn2.example.com"
    token      TEXT NOT NULL DEFAULT '',   -- raw API token (ws_...) for Bearer auth
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
```

Рисков нет — новая таблица, ничего не ломает.

---

## Шаг 2 — Новый пакет `internal/remotes/` (medium)

**Новый файл:** `internal/remotes/remotes.go`

### Структуры

```go
type Remote struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    URL       string    `json:"url"`
    CreatedAt time.Time `json:"created_at"`
    // Token намеренно отсутствует в JSON — не возвращается на фронт
}

type RemoteWithToken struct {
    Remote
    Token string
}
```

### Функции пакета

| Функция | Описание | Complexity |
|---------|----------|-----------|
| `List() ([]Remote, error)` | Все remotes, ORDER BY created_at | small |
| `GetByID(id string) (*RemoteWithToken, error)` | По ID, включая токен | small |
| `Create(name, url, token string) (*Remote, error)` | INSERT, uuid v4 | small |
| `Delete(id string) error` | DELETE by ID | small |
| `UpdateToken(id, token string) error` | Обновить токен (refresh) | small |

---

## Шаг 3 — Новый пакет `internal/remoteclient/` (medium)

**Новый файл:** `internal/remoteclient/client.go`

Отвечает за HTTP-взаимодействие с Remote Cascade:

### Функции

```go
// Login: POST /api/session на remote, получить session cookie
// CreateToken: POST /api/tokens на remote с session cookie
// Logout: DELETE /api/session на remote
// ObtainToken(url, username, password string) (token string, err error)
//   — комбинирует Login + CreateToken + Logout
```

Детали `ObtainToken`:
1. `POST <url>/api/session` с `{username, password}` → получить `Set-Cookie: session_id=...`
2. `POST <url>/api/tokens` с cookie + `{name: "cascade-managed"}` → получить `{token: {raw: "ws_..."}}`
3. `DELETE <url>/api/session` с cookie → logout
4. Вернуть raw token

Использует стандартный `net/http` с `http.Client{Timeout: 15s}`.  
Заголовки: `Content-Type: application/json`.  
Нужно вручную обрабатывать `Set-Cookie` и передавать cookie в следующие запросы (через `CookieJar`).

**Edge cases:**
- Remote недоступен → timeout → понятная ошибка пользователю
- Remote вернул TOTP_required → вернуть ошибку "Remote requires 2FA; use API token instead"
- Неверные credentials → 401 → ошибка пользователю
- Remote URL с trailing slash — нормализовать через `strings.TrimRight(url, "/")`

---

## Шаг 4 — HTTP handlers `internal/api/remotes.go` (medium)

**Новый файл:** `internal/api/remotes.go`

### Эндпоинты

```
GET    /api/remotes               → { remotes: [...] }  (без токенов)
POST   /api/remotes               → { remote: {...} }   (login + token получение)
DELETE /api/remotes/:id           → 204
POST   /api/remotes/:id/refresh   → { remote: {...} }   (переполучить токен)
GET    /api/remotes/:id/test      → { ok: true, version: "..." }
ALL    /api/remotes/:id/proxy/*   → proxy pass на remote
```

### Функция RegisterRemotes(api fiber.Router)

Все маршруты за AuthMiddleware.

### POST /api/remotes — детали

Body: `{ name, url, username, password }`

1. Вызвать `remoteclient.ObtainToken(url, username, password)`
2. При ошибке → 400/502 с понятным сообщением
3. При успехе → `remotes.Create(name, url, token)`
4. Вернуть `{ remote: {...} }` без токена

Пароль нигде не логируется, нигде не сохраняется.

### GET /api/remotes/:id/test — детали

1. `remotes.GetByID(id)` → получить токен
2. `GET <remote.URL>/api/health` с `Authorization: Bearer <token>`
3. Если 200 → `{ ok: true, version: "..." }`
4. Если 401 → `{ ok: false, error: "token expired" }`

### ALL /api/remotes/:id/proxy/* — proxy handler (КЛЮЧЕВОЙ)

```
/api/remotes/:id/proxy/tunnel-interfaces
         ↓ прокси до
<remote>/api/tunnel-interfaces
```

Алгоритм:
1. `remotes.GetByID(id)` → `remoteURL + token`
2. Извлечь подпуть: `c.Params("*")` → `/tunnel-interfaces`
3. Построить целевой URL: `remoteURL + "/api/" + subpath`
4. Скопировать query string
5. Создать `http.Request` с методом, телом и заголовками оригинального запроса
6. Добавить `Authorization: Bearer <token>`
7. Убрать `Cookie` заголовок (не нужен на remote)
8. Выполнить запрос (`http.Client{Timeout: 30s}`)
9. Скопировать response status + body → ответить клиенту

**Важно:** Fiber route: `api.All("/remotes/:id/proxy/*", proxyHandler)`.  
В Fiber wildcard `*` доступен через `c.Params("*")`.

**Edge cases:**
- 401 от remote → вернуть 502 с `{ error: "remote token invalid, please re-add the server" }`
- Таймаут → 504
- Remote сертификат самоподписанный — пока `InsecureSkipVerify: false` (строгий режим). Если нужно — можно добавить опцию в будущем.
- Большие тела (binary downloads config/qr) — читать через `io.Copy` без буферизации

---

## Шаг 5 — Регистрация в main.go (small)

**Файл:** `cmd/awg-easy/main.go`

Добавить после `api.RegisterGateways(apiGroup)`:

```go
api.RegisterRemotes(apiGroup)
```

Никакой инициализации менеджера не нужно — пакет `remotes` работает напрямую с `db.DB()`.

---

## Шаг 6 — API клиент фронтенда (small)

**Файл:** `internal/frontend/www/js/api.js`

Добавить методы в класс `API`:

```javascript
// Remotes
async getRemotes()                          // GET /api/remotes
async addRemote(name, url, username, password) // POST /api/remotes
async deleteRemote(id)                      // DELETE /api/remotes/:id
async testRemote(id)                        // GET /api/remotes/:id/test
async refreshRemoteToken(id, username, password) // POST /api/remotes/:id/refresh

// Remote proxy — все вызовы через prefix /remotes/:id/proxy
// Достаточно передавать remoteId, тогда существующие методы
// перегружаются с параметром remoteId (альтернативный подход):
// getRemoteInterfaces(remoteId)
// getRemotePeers(remoteId, ifaceId)
// и т.д.
```

**Альтернативный дизайн (рекомендуемый):** Добавить метод `remoteCall(remoteId, {method, path, body})` который строит URL с `/remotes/:remoteId/proxy` префиксом, а затем добавить методы-обёртки для нужных операций.

---

## Шаг 7 — UI: страница Remotes (large)

**Файл:** `internal/frontend/www/js/app.js`

### Данные

```javascript
// в data: {}
remotes: [],                  // список серверов
showAddRemote: false,         // модалка добавления
addRemoteForm: { name: '', url: '', username: '', password: '' },
addRemoteLoading: false,
addRemoteError: '',
remoteContext: null,          // { id, name } — активный remote или null (= local)
```

### Методы

```javascript
loadRemotes()          // GET /api/remotes → this.remotes
addRemote()            // POST /api/remotes → закрыть модалку → reload
deleteRemote(id)       // DELETE → reload
testRemote(id)         // GET test → показать статус
switchToRemote(remote) // this.remoteContext = remote; loadRemoteInterfaces()
switchToLocal()        // this.remoteContext = null; loadInterfaces()
loadRemoteInterfaces() // через proxy API
```

### Sidebar

Добавить в `sidebarMenu` новую запись:

```javascript
{ id: 'remotes', label: 'Remote Servers' }
```

Разместить между `administration` и `settings` или после `administration`.

### Режим работы с удалённым сервером

Когда `remoteContext !== null`:
- В шапке показывается badge "Remote: <name>" с кнопкой "Back to local"
- `loadInterfaces()` в polling: если `remoteContext` → использовать `remoteCall`
- Страница Interfaces работает через proxy как обычно

**Важный UX вопрос:** Какой scope переключается при смене remote?  
Рекомендация: переключение remote меняет контекст только страницы Interfaces (не Dashboard, не Firewall). Это проще в реализации и понятнее пользователю.

---

## Шаг 8 — UI: страница Remotes (index.html) (medium)

**Файл:** `internal/frontend/www/index.html`

Добавить секцию `v-if="activePage === 'remotes'"`:

```
Список серверов:
  [ Name ]  [ URL ]  [ Status ]  [ Test ]  [ Delete ]

Кнопка "+ Add Remote Server" → открывает модалку

Модалка Add Remote:
  - Name (text)
  - URL (text, placeholder "https://vpn2.example.com")
  - Username (text)
  - Password (password)
  - Кнопки: Cancel | Add Server (loading state)
  - Error message если не удалось подключиться
```

Использовать паттерн модалок из CLAUDE.md (fixed inset-0 bg-black bg-opacity-50).

---

## Шаг 9 — S2S Wizard (large)

**Файл новый:** нет — всё в index.html и app.js

### Условия появления кнопки

На странице Interfaces, в карточке/деталях интерфейса: кнопка "S2S Wizard" (только если `remotes.length > 0`).

### Данные

```javascript
showS2SWizard: false,
s2sWizard: {
  localIfaceId: null,
  remoteId: null,
  remoteIfaceId: null,
  psk: '',        // сгенерированный PSK
  step: 1,        // 1=выбор, 2=подтверждение, 3=результат
  loading: false,
  error: '',
  result: null,   // { localPeer, remotePeer }
}
```

### Шаги Wizard

**Шаг 1 — Выбор:**
- Локальный интерфейс (select из `tunnelInterfaces`)
- Удалённый сервер (select из `remotes`)
- Удалённый интерфейс (select, подгружается при выборе remote)

**Шаг 2 — Подтверждение:**
- Показать что будет создано:
  - Local peer на `localIface` → будет peer с AllowedIPs = remote iface address/32
  - Remote peer на `remoteIface` → будет peer с AllowedIPs = local iface address/32
  - PSK: `[generate]` кнопка

**Шаг 3 — Результат:**
- Показать созданные peer'ы, статус, кнопка "Close"

### Логика createS2S() в app.js

```javascript
async createS2S() {
  // 1. Получить параметры локального интерфейса
  //    GET /api/tunnel-interfaces/:localIfaceId/export-params (public key, endpoint, etc.)
  // 2. Получить параметры удалённого интерфейса
  //    GET /api/remotes/:remoteId/proxy/tunnel-interfaces/:remoteIfaceId/export-params
  // 3. Сгенерировать PSK (либо на сервере — новый endpoint GET /api/util/generate-psk,
  //    либо через существующий export-json flow)
  // 4. POST /api/tunnel-interfaces/:localIfaceId/peers/import-json
  //    с данными remote iface (public key, endpoint, allowed IPs)
  // 5. POST /api/remotes/:remoteId/proxy/tunnel-interfaces/:remoteIfaceId/peers/import-json
  //    с данными local iface
  // 6. Показать результат
}
```

**PSK генерация:** Добавить endpoint `GET /api/util/generate-key` (или переиспользовать что уже есть). Проверить есть ли уже что-то в существующих handlers.

---

## Порядок реализации

| # | Шаг | Файлы | Complexity | Зависимости |
|---|-----|-------|-----------|-------------|
| 1 | DB migration v30 | `internal/db/db.go` | small | — |
| 2 | Пакет remotes | `internal/remotes/remotes.go` | small | Шаг 1 |
| 3 | Пакет remoteclient | `internal/remoteclient/client.go` | medium | — |
| 4 | API handlers remotes.go | `internal/api/remotes.go` | medium | Шаг 2, 3 |
| 5 | Регистрация в main.go | `cmd/awg-easy/main.go` | small | Шаг 4 |
| 6 | api.js методы | `js/api.js` | small | Шаг 4 |
| 7 | app.js data+methods | `js/app.js` | medium | Шаг 6 |
| 8 | index.html страница Remotes | `index.html` | medium | Шаг 7 |
| 9 | S2S Wizard (app.js + html) | `js/app.js`, `index.html` | large | Шаг 7, 8 |

---

## Файлы, которые будут изменены

| Файл | Тип изменения |
|------|--------------|
| `internal/db/db.go` | Добавить migration v30 |
| `cmd/awg-easy/main.go` | Добавить `api.RegisterRemotes(apiGroup)` |
| `internal/frontend/www/js/api.js` | Добавить методы для remotes + remoteCall |
| `internal/frontend/www/js/app.js` | Добавить data, методы, polling logic |
| `internal/frontend/www/index.html` | Добавить страницу Remotes + S2S Wizard модалку |

**Файлы НЕ должны модифицироваться (только читать):**
- `internal/api/auth.go` — AuthMiddleware используется как есть
- `internal/tokens/tokens.go` — логика токенов не меняется
- `internal/firewall/manager.go`, `internal/tunnel/` — без изменений

---

## Новые файлы

| Файл | Роль |
|------|------|
| `internal/remotes/remotes.go` | CRUD для remotes в SQLite |
| `internal/remoteclient/client.go` | HTTP-клиент для login+token+proxy |
| `internal/api/remotes.go` | HTTP handlers + proxy endpoint |

---

## Риски и edge cases

### Безопасность
- **Токен хранится plain-text** — это осознанное решение (как и в большинстве self-hosted систем). Альтернатива — шифровать токен ключом из PASSWORD_HASH. Но это усложняет код без существенного прироста безопасности (кто имеет доступ к DB — имеет доступ к системе). Оставить plain-text.
- **SSRF через proxy endpoint** — пользователь мог бы добавить `url=http://169.254.169.254/` (AWS metadata). Митигация: валидировать URL при добавлении (должен быть http/https, не localhost/link-local). Добавить `validate.RemoteURL(url)`.
- **Proxy пробрасывает все заголовки** — нужно вайтлистить или явно дропать Host, Connection, Upgrade заголовки.

### Совместимость
- Если remote — старая версия Cascade без токенов → `POST /api/tokens` вернёт 404. Нужно обработать и показать понятную ошибку.
- Remote с TOTP → `ObtainToken` вернёт `{ totp_required: true }` → обработать как ошибку "2FA not supported for managed servers".

### Proxy streaming
- `/api/tunnel-interfaces/:id/peers/:peerId/config` и `/qrcode.svg` — binary/text ответы. Нужно копировать `Content-Type` заголовок из remote ответа.

### S2S Wizard
- `export-params` endpoint существует? Проверить: `grep -n "export-params" internal/api/interfaces.go`. Если нет — Wizard упрётся в это. Может потребоваться добавить.
- PSK должен быть одинаковым на обоих концах — нельзя генерировать на каждом сервере независимо. Генерировать один раз на local, передавать в оба `import-json` запроса.

### Polling
- Текущий polling (`setInterval`) вызывает `loadInterfaces()` каждую секунду. В remote-режиме это будет делать HTTP-запрос через proxy на каждый тик — добавляет ~50-100ms latency но в целом допустимо. Альтернатива: увеличить polling interval для remote context до 5s.

---

## Дополнительно: generate-key endpoint

Проверить существует ли `/api/util/generate-key` или аналог:

```bash
grep -rn "generate.*key\|GenerateKey" internal/api/
```

Если нет — нужен маленький endpoint для S2S Wizard:

```
GET /api/util/generate-psk → { psk: "base64..." }
```

Реализуется в `internal/api/system.go` (5 строк).

---

## Tailwind CSS — новые страницы

Перед верстальными работами обязательно:
```bash
grep "классы которые планируешь использовать" internal/frontend/www/css/app.css
```

Для модалок использовать pattern из CLAUDE.md. Все отсутствующие классы — через `style="..."`.
