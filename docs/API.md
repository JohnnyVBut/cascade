# WireSteer — API Reference (Go Rewrite)

> **Base URL:** `/api`
> **Auth:** Все маршруты кроме session, lang, release, remember-me и UI-флагов требуют либо валидного session cookie, либо API-токена (`Authorization: Bearer ws_...`).
> **Content-Type:** `application/json`

---

## Аутентификация

### Сессия (Web UI)

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/session` | Текущее состояние сессии. Возвращает `{ authenticated, requiresPassword, totp_pending, username }` |
| `POST` | `/api/session` | Логин шаг 1. Body: `{ username, password, remember? }`. Возвращает `{ authenticated: true }` или `{ totp_required: true }` |
| `DELETE` | `/api/session` | Логаут |
| `POST` | `/api/auth/totp/verify` | Логин шаг 2 (TOTP). Body: `{ code }`. Возвращает `{ authenticated: true }`. Требует `totp_pending` сессии. |

### Управление пользователями

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/users` | Список пользователей. Возвращает `{ users: [...] }` |
| `POST` | `/api/users` | Создать пользователя. Body: `{ username, password }`. Возвращает `{ user }` |
| `GET` | `/api/users/me` | Текущий пользователь |
| `PATCH` | `/api/users/me` | Изменить свой пароль. Body: `{ password }` |
| `PATCH` | `/api/users/:id` | Обновить username или пароль. Body: `{ username?, password? }` |
| `DELETE` | `/api/users/:id` | Удалить пользователя (нельзя удалить последнего) |

### TOTP (2FA)

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/users/me/totp/setup` | Сгенерировать TOTP secret. Возвращает `{ secret, qr_uri, qr_png }`. Secret хранится в сессии до подтверждения. |
| `POST` | `/api/users/me/totp/enable` | Подтвердить и активировать TOTP. Body: `{ code }` |
| `POST` | `/api/users/me/totp/disable` | Отключить TOTP. Body: `{ code }` (текущий TOTP-код) |

### API-токены (программный доступ)

Долгоживущие токены для скриптов и автоматизации. TOTP не требуется.
Формат токена: `ws_` + 64 hex-символа. В БД хранится только SHA-256 хеш — raw-значение показывается единожды при создании.

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/tokens` | Список токенов текущего пользователя. Возвращает `{ tokens: [{id, name, last_used, created_at}] }` |
| `POST` | `/api/tokens` | Создать токен. Body: `{ name }`. Возвращает `{ token, raw_token }` — `raw_token` показывается **один раз** |
| `DELETE` | `/api/tokens/:id` | Отозвать токен |

**Использование:**
```bash
# Логин через сессию
curl -c /tmp/ws.cookie -X POST https://<IP>/<ADMIN_PATH>/api/session \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"..."}'

# API-токен (без сессии, без TOTP)
curl -H "Authorization: Bearer ws_<токен>" \
  https://<IP>/<ADMIN_PATH>/api/tunnel-interfaces
```

---

## Настройки

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/settings` | Глобальные настройки |
| `PUT` | `/api/settings` | Частичное обновление. Body: `{ dns?, defaultPersistentKeepalive?, defaultClientAllowedIPs?, gatewayWindowSeconds?, gatewayHealthyThreshold?, gatewayDegradedThreshold? }` |

---

## AWG2 Шаблоны

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/templates` | Список шаблонов |
| `POST` | `/api/templates` | Создать шаблон. Body: `{ name, jc, jmin, jmax, s1–s4, h1–h4, i1–i5 }` |
| `GET` | `/api/templates/:id` | Получить шаблон |
| `PUT` | `/api/templates/:id` | Обновить шаблон |
| `DELETE` | `/api/templates/:id` | Удалить шаблон |
| `POST` | `/api/templates/:id/set-default` | Сделать дефолтным |
| `POST` | `/api/templates/:id/apply` | Применить — возвращает AWG2 параметры со свежими H1-H4 |
| `POST` | `/api/templates/generate` | Сгенерировать AWG2 параметры. Body: `{ profile, intensity, host, saveName? }` |

---

## Tunnel Interfaces (Интерфейсы)

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/tunnel-interfaces` | Список интерфейсов. Возвращает `{ interfaces: [...] }` |
| `POST` | `/api/tunnel-interfaces` | Создать. Body: `{ name, address, listenPort, protocol, disableRoutes?, settings? }` |
| `GET` | `/api/tunnel-interfaces/:id` | Получить интерфейс |
| `PATCH` | `/api/tunnel-interfaces/:id` | Обновить (hot-reload через syncconf). Body: частичные поля |
| `DELETE` | `/api/tunnel-interfaces/:id` | Удалить интерфейс |
| `POST` | `/api/tunnel-interfaces/:id/start` | Запустить. Возвращает `{ interface }` |
| `POST` | `/api/tunnel-interfaces/:id/stop` | Остановить. Возвращает `{ interface }` |
| `POST` | `/api/tunnel-interfaces/:id/restart` | Перезапустить. Возвращает `{ interface }` |
| `GET` | `/api/tunnel-interfaces/:id/export-params` | Экспорт параметров для S2S. Возвращает `{ name, publicKey, endpoint, address, protocol, presharedKey? }` |
| `GET` | `/api/tunnel-interfaces/:id/export-obfuscation` | Экспорт AWG2 параметров обфускации как JSON |
| `GET` | `/api/tunnel-interfaces/:id/backup` | Скачать бэкап интерфейса + всех пиров |
| `PUT` | `/api/tunnel-interfaces/:id/restore` | Восстановить пиров из бэкапа. Сначала удаляет существующих пиров |

---

## Пиры (Peers)

Базовый путь: `/api/tunnel-interfaces/:id/peers`

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/peers` | Список пиров. Возвращает `{ peers: [...] }` |
| `POST` | `/peers` | Создать пира. Body: `{ name, peerType (client/interconnect), clientAllowedIPs?, persistentKeepalive?, expiredAt? }` |
| `POST` | `/peers/import-json` | Создать interconnect-пира из экспортированного JSON |
| `GET` | `/peers/:peerId` | Получить пира |
| `PATCH` | `/peers/:peerId` | Обновить поля пира |
| `DELETE` | `/peers/:peerId` | Удалить пира |
| `GET` | `/peers/:peerId/config` | Скачать WireGuard config файл |
| `GET` | `/peers/:peerId/qrcode.svg` | QR-код SVG (только client-пиры) |
| `POST` | `/peers/:peerId/enable` | Включить пира |
| `POST` | `/peers/:peerId/disable` | Выключить пира |
| `PUT` | `/peers/:peerId/name` | Переименовать пира. Body: `{ name }` |
| `PUT` | `/peers/:peerId/address` | Обновить overlay-адрес. Body: `{ address }` → сохраняется как AllowedIPs |
| `PUT` | `/peers/:peerId/expireDate` | Установить дату истечения. Body: `{ expireDate }` — RFC3339 или YYYY-MM-DD, пустое = сбросить |
| `POST` | `/peers/:peerId/generateOneTimeLink` | Сгенерировать одноразовый токен для конфига |
| `GET` | `/peers/:peerId/export-json` | Экспорт interconnect-пира как JSON (только interconnect) |

---

## Маршрутизация (Routing)

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/routing/table` | Маршруты ядра. Query: `?table=main` (по умолчанию) |
| `GET` | `/api/routing/tables` | Таблицы маршрутизации из `ip rule show`. Возвращает `{ tables: [...] }` |
| `GET` | `/api/routing/test` | Тест маршрута. Query: `?ip=<dst>[&src=<src>][&mark=<fwmark>]`. С `src`: SimulateTrace (PBR) → `ip route get <dst> mark <fwmark>`. Возвращает `{ result, matchedRule, steps }` |
| `GET` | `/api/routing/routes` | Статические маршруты (из БД). Возвращает `{ routes: [...] }` |
| `POST` | `/api/routing/routes` | Создать маршрут. Body: `{ destination, via?, dev?, metric?, table?, comment? }` |
| `PATCH` | `/api/routing/routes/:id` | Обновить или переключить: `{ enabled: bool }` |
| `DELETE` | `/api/routing/routes/:id` | Удалить маршрут |

---

## NAT

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/nat/interfaces` | Сетевые интерфейсы хоста. Возвращает `{ interfaces: [...] }` |
| `GET` | `/api/nat/rules` | NAT-правила + авто-правила от интерфейсов. Возвращает `{ rules: [...] }`. Авто-правила имеют `"auto": true` (только чтение) |
| `POST` | `/api/nat/rules` | Создать правило. Body: `{ name, source?, sourceAliasId?, outInterface, type (MASQUERADE/SNAT), toSource? (только SNAT), comment? }` |
| `PATCH` | `/api/nat/rules/:id` | Обновить или переключить: `{ enabled: bool }` |
| `DELETE` | `/api/nat/rules/:id` | Удалить правило |

---

## Шлюзы (Gateways)

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/gateways` | Список шлюзов с live-статусом. Возвращает `{ gateways: [...] }` |
| `POST` | `/api/gateways` | Создать шлюз. Body: `{ name, interface, gatewayIP, monitorAddress?, interval?, windowSeconds?, healthyThreshold?, degradedThreshold?, monitorHttp? }` |
| `GET` | `/api/gateways/:id` | Получить шлюз |
| `PATCH` | `/api/gateways/:id` | Обновить шлюз |
| `DELETE` | `/api/gateways/:id` | Удалить шлюз |

### Группы шлюзов (Gateway Groups)

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/gateway-groups` | Список групп. Возвращает `{ groups: [...] }` |
| `POST` | `/api/gateway-groups` | Создать группу. Body: `{ name, members: [{gatewayId, tier}], trigger (packetloss/latency/packetloss_latency) }` |
| `GET` | `/api/gateway-groups/:id` | Получить группу |
| `PATCH` | `/api/gateway-groups/:id` | Обновить группу |
| `DELETE` | `/api/gateway-groups/:id` | Удалить группу |

---

## Файрвол (Firewall)

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/firewall/interfaces` | Интерфейсы хоста для привязки правил. Возвращает `{ interfaces: [...] }` |
| `GET` | `/api/firewall/rules` | Правила, отсортированные по `order`. Возвращает `{ rules: [...] }` |
| `POST` | `/api/firewall/rules` | Создать правило. Body: `{ name?, interface?, protocol?, source (Endpoint), destination (Endpoint), action (accept/drop/reject), gatewayId?, gatewayGroupId?, fallbackToDefault?, comment?, enabled? }` |
| `PATCH` | `/api/firewall/rules/:id` | Обновить или переключить: `{ enabled: bool }` |
| `DELETE` | `/api/firewall/rules/:id` | Удалить правило |
| `POST` | `/api/firewall/rules/:id/move` | Переместить правило. Body: `{ direction: "up"\|"down" }` |

### Структура Endpoint

```json
{
  "type": "any | cidr | alias",
  "value": "10.0.0.0/8",
  "aliasId": "<uuid>",
  "portAliasId": "<uuid>",
  "invert": false
}
```

---

## Алиасы (Aliases)

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/aliases` | Список алиасов. Возвращает `{ aliases: [...] }` |
| `POST` | `/api/aliases` | Создать алиас. Body: `{ name, type, entries?, comment? }` |
| `GET` | `/api/aliases/:id` | Получить алиас |
| `PATCH` | `/api/aliases/:id` | Обновить алиас |
| `DELETE` | `/api/aliases/:id` | Удалить алиас |
| `POST` | `/api/aliases/:id/upload` | Загрузить список префиксов. Body: `{ content: "..." }` |
| `POST` | `/api/aliases/:id/generate` | Сгенерировать ipset из RIPE/ipdeny. Body: `{ country?, asn?, asnList? }`. Возвращает `{ jobId }` |
| `GET` | `/api/aliases/:id/generate/:jobId` | Статус задачи генерации. Возвращает `{ status: "running"\|"done"\|"error", entryCount?, error? }` |

### Типы алиасов

| Тип | Формат entries | Использование |
|-----|---------------|---------------|
| `host` | `["1.2.3.4"]` | Одиночные IP |
| `network` | `["10.0.0.0/8"]` | CIDR-диапазоны |
| `ipset` | генерируется | Большие наборы префиксов (kernel ipset) |
| `group` | `["<aliasId>"]` | Объединяет host/network-алиасы |
| `port` | `["tcp:443", "udp:53", "any:80"]` | L4-порты |
| `port-group` | `["<portAliasId>"]` | Объединяет port-алиасы |

---

## Заглушки совместимости (Compat Stubs)

Эндпоинты из Node.js-версии, сохранённые для совместимости с фронтендом. Только чтение, возвращают безопасные дефолты.

### Без аутентификации

| Метод | Путь | Возвращает |
|-------|------|-----------|
| `GET` | `/api/lang` | `"en"` |
| `GET` | `/api/release` | `999999` (подавляет баннер обновления) |
| `GET` | `/api/remember-me` | `true` |
| `GET` | `/api/ui-traffic-stats` | `false` |
| `GET` | `/api/ui-chart-type` | `0` |
| `GET` | `/api/wg-enable-one-time-links` | `false` |
| `GET` | `/api/ui-sort-clients` | `false` |
| `GET` | `/api/wg-enable-expire-time` | `false` |
| `GET` | `/api/ui-avatar-settings` | `{ dicebear: null, gravatar: false }` |

### С аутентификацией

| Метод | Путь | Возвращает |
|-------|------|-----------|
| `GET` | `/api/wireguard/client` | `[]` — admin-туннель не реализован |
| `ALL` | `/api/wireguard/*` | `501 Not Implemented` |
| `GET` | `/api/system/interfaces` | `{ interfaces: [...] }` — интерфейсы хоста |

---

## Соглашения по ответам

- Все list-эндпоинты возвращают **именованную обёртку**: `{ peers/interfaces/rules/routes/... : [...] }` — никогда не голый массив
- Ошибки: `{ error: "message" }` с соответствующим HTTP статусом (400 / 401 / 404 / 500)
- Toggle через PATCH: `{ enabled: true|false }` — остальные поля не нужны
- Временны́е метки: RFC3339 UTC — `"2026-03-19T10:00:00Z"`
- ID интерфейсов: строковые слаги — `"wg10"`, `"wg11"`, …
- Все остальные ID: UUID v4
