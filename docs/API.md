# Cascade — API Reference (Go Rewrite)

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
| `POST` | `/api/users/:id/set-admin` | Назначить/снять роль admin. Body: `{ admin: bool }`. Только для admin. Нельзя снять роль с последнего admin |

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

## Версия и обновления

| Метод | Путь | Auth | Описание |
|-------|------|------|----------|
| `GET` | `/api/version` | ❌ публичный | Текущая версия + инфо о последнем релизе с GitHub. Ответ: `{ version, gitCommit, latestVersion, releaseURL, updateAvailable: bool, checkedAt, error? }` |
| `POST` | `/api/version/check` | ❌ публичный | Принудительная проверка релиза на GitHub, минуя кэш 24 ч. Возвращает то же, что и `GET /api/version`. |
| `GET` | `/api/health` | ❌ публичный | Health-check. Ответ: `{ status: "ok", version, host }` |

`version` равен `"dev"` для локальных сборок без ldflags. Инжектируется при сборке через:
```
-ldflags "-X ...version.Version=v1.2.3 -X ...version.GitCommit=abc1234"
```
Проверка обновлений поллит `https://api.github.com/repos/JohnnyVBut/cascade/releases/latest` раз в 24 ч.
Первая проверка — через 10 с после старта. Результат кэшируется в памяти — `/api/version` всегда отвечает мгновенно.

---

## Настройки

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/settings` | Глобальные настройки + runtime-информация |
| `PUT` | `/api/settings` | Частичное обновление. Body: см. ниже |

**GET /api/settings — поля ответа:**

Возвращает `GlobalSettings` + runtime-only поля:

| Поле | Тип | Описание |
|------|-----|----------|
| `dns` | string | DNS-сервер для клиентских конфигов |
| `mtu` | int | MTU для клиентских конфигов. `0` = не задан (WireGuard выбирает автоматически). Диапазон: 576–9000 |
| `defaultPersistentKeepalive` | int | Keepalive по умолчанию (сек) |
| `defaultClientAllowedIPs` | string | AllowedIPs для новых клиентских пиров |
| `gatewayWindowSeconds` | int | Скользящее окно мониторинга шлюзов (сек) |
| `gatewayHealthyThreshold` | int | Порог healthy (% потерь пакетов) |
| `gatewayDegradedThreshold` | int | Порог degraded (% потерь пакетов) |
| `subnetPool` | string | CIDR-пул для авто-назначения подсетей при quick-create, напр. `"192.168.0.0/16"`. Невалидное значение → **400** |
| `portPool` | string | Пул портов для quick-create, напр. `"51831-65535"` (диапазоны и запятые). Невалидное значение → **400** |
| `defaultFwPolicy` | string | Дефолтная политика файрвола: `"accept"` или `"drop"`. По умолчанию `"accept"` |
| `routerName` | string | Человекочитаемое имя роутера (отображается в сайдбаре) |
| `publicIPMode` | string | Режим определения публичного IP: `"auto"` или `"manual"` |
| `publicIPManual` | string | Ручной публичный IP (используется при `publicIPMode="manual"`) |
| `chartType` | int | Тип графиков трафика: `0`=выкл, `1`=line, `2`=area, `3`=bar |
| `hostname` | string | *(runtime)* Имя хоста контейнера |
| `resolvedPublicIP` | string | *(runtime)* Разрешённый публичный IP для endpoint |
| `publicIPWarning` | string | *(runtime)* Предупреждение если публичный IP недоступен |
| `awgMode` | string | *(runtime)* `"kernel"` или `"userspace"` (amneziawg-go) |
| `networkMode` | string | *(runtime)* `"host"`, `"bridge"` или `"none"` — Docker network mode |

**PUT /api/settings — принимаемые поля:**

`{ dns?, mtu?, defaultPersistentKeepalive?, defaultClientAllowedIPs?, gatewayWindowSeconds?, gatewayHealthyThreshold?, gatewayDegradedThreshold?, subnetPool?, portPool?, defaultFwPolicy?, routerName?, publicIPMode?, publicIPManual?, chartType?, lang? }`

`lang` — язык UI: `"en"` или `"ru"`. Также отражается в `GET /api/lang`.

`mtu` — глобальный MTU для клиентских конфигов. Может быть переопределён на уровне конкретного интерфейса.

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
| `POST` | `/api/templates/generate` | Сгенерировать AWG2 параметры. Body: `{ profile, intensity, host?, browser?, saveName? }`. profile: random|quic_initial|quic_0rtt|tls_client_hello|dtls|http3|sip|wireguard_noise|**dns_query**|tls_to_quic|quic_burst. browser: chrome|firefox|safari|edge|yandex_desktop|yandex_mobile (не применяется для sip и dns_query) |

---

## Tunnel Interfaces (Интерфейсы)

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/tunnel-interfaces` | Список интерфейсов. Возвращает `{ interfaces: [...] }` |
| `POST` | `/api/tunnel-interfaces` | Создать. Body: `{ name, address, listenPort, protocol, disableRoutes?, natDisabled?, settings? }` |
| `POST` | `/api/tunnel-interfaces/quick-create` | Quick-create: создать и запустить клиентский интерфейс одной командой. Body: `{ name?: string, protocol?: string }`. Адрес и порт назначаются автоматически из SubnetPool/PortPool. AWG2 параметры — из шаблона по умолчанию или random. Ответ: `{ interface, started: bool, startError?: string }` |
| `POST` | `/api/tunnel-interfaces/import-conf` | Импорт клиентского `.conf` файла WireGuard/AmneziaWG как аплинк-интерфейс. `DisableRoutes` всегда `true` — таблица маршрутизации не изменяется. Body: `{ name: string, conf: string }`. Ответ: `{ interface, peer, started: bool, startError?: string, conflictWarning?: string }` |
| `POST` | `/api/tunnel-interfaces/import-backup` | Импорт бэкапа AWG-Easy. Создаёт новый интерфейс со всеми клиентами из файла. Ключи сервера и клиентов сохраняются as-is — существующие конфиги клиентов остаются валидными. Body: `{ json: string, listenPort: int }`. Ответ: `{ interface, peersCreated: int, peersFailed?: string[], started: bool, startError?: string }`. Конфликт порта или подсети → **400** |
| `GET` | `/api/tunnel-interfaces/:id` | Получить интерфейс |
| `PATCH` | `/api/tunnel-interfaces/:id` | Обновить (hot-reload через syncconf). Body: `{ name?, address?, listenPort?, natDisabled?, publicHost?, mtu?, settings? }`. `publicHost` переопределяет глобальный Public IP для конфигов пиров этого интерфейса (для транзит/relay). `mtu` переопределяет глобальный MTU (`0` = использовать глобальный). Изменение `natDisabled` при запущенном интерфейсе вызывает `Restart()` |
| `DELETE` | `/api/tunnel-interfaces/:id` | Удалить интерфейс |
| `POST` | `/api/tunnel-interfaces/:id/start` | Запустить. Возвращает `{ interface }` |
| `POST` | `/api/tunnel-interfaces/:id/stop` | Остановить. Возвращает `{ interface }` |
| `POST` | `/api/tunnel-interfaces/:id/restart` | Перезапустить. Возвращает `{ interface }` |
| `GET` | `/api/tunnel-interfaces/:id/export-params` | Экспорт параметров для S2S. Возвращает `{ name, publicKey, endpoint, address, protocol, presharedKey? }` |
| `GET` | `/api/tunnel-interfaces/:id/export-obfuscation` | Экспорт AWG2 параметров обфускации как JSON |
| `GET` | `/api/tunnel-interfaces/:id/export` | Экспорт полного интерфейса (включая приватный ключ) + опционально всех пиров как JSON — для клонирования/переноса интерфейса на другой сервер. Query: `?peers=0`, чтобы не включать пиров (по умолчанию включены) |
| `POST` | `/api/tunnel-interfaces/import-interface` | Импорт интерфейса, ранее экспортированного через `GET /:id/export`. Body: `{ json: string, listenPort: int }` |
| `GET` | `/api/tunnel-interfaces/:id/backup` | Скачать бэкап интерфейса + всех пиров |
| `PUT` | `/api/tunnel-interfaces/:id/restore` | Восстановить пиров из бэкапа. Сначала удаляет существующих пиров |

---

## Пиры (Peers)

Базовый путь: `/api/tunnel-interfaces/:id/peers`

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/peers` | Список пиров. Возвращает `{ peers: [...] }` |
| `POST` | `/peers` | Создать пира. Body: `{ name, peerType (client/interconnect), clientAllowedIPs?, persistentKeepalive?, expiredAt? }`. Ответ содержит `totalRx`/`totalTx` (lifetime-счётчики трафика из SQLite) |
| `POST` | `/peers/import-json` | Создать interconnect-пира из экспортированного JSON |
| `POST` | `/peers/import-client-configs` | Сопоставить загруженные клиентские `.conf`-файлы WireGuard с существующими пирами по публичному ключу (вычисляется из приватного ключа файла) и сохранить приватный ключ — разблокирует QR-код/скачивание конфига для пиров, созданных без него (например, импортированных из бэкапа AWG-Easy). Multipart-поле `configs` (несколько файлов) |
| `GET` | `/peers/:peerId` | Получить пира |
| `PATCH` | `/peers/:peerId` | Обновить поля пира. Принимает: `name?, endpoint?, allowedIPs?, clientAllowedIPs?, persistentKeepalive?, enabled?, expiredAt?, oneTimeLink?, rateDown?, rateUp?`. Поля `rateDown`/`rateUp` — ограничение скорости в **кбит/с** (0 = без ограничений), применяется через `tc HTB + police` на сервере; в UI вводится в **Мбит/с** и конвертируется автоматически |
| `DELETE` | `/peers/:peerId` | Удалить пира |
| `GET` | `/peers/:peerId/config` | Скачать WireGuard config файл |
| `GET` | `/peers/:peerId/qrcode.svg` | QR-код SVG (только client-пиры) |
| `POST` | `/peers/:peerId/enable` | Включить пира |
| `POST` | `/peers/:peerId/disable` | Выключить пира |
| `PUT` | `/peers/:peerId/name` | Переименовать пира. Body: `{ name }` |
| `PUT` | `/peers/:peerId/address` | Обновить overlay-адрес. Body: `{ address }` → сохраняется как AllowedIPs |
| `PUT` | `/peers/:peerId/expireDate` | Установить дату истечения. Body: `{ expireDate }` — RFC3339 или YYYY-MM-DD, пустое = сбросить |
| `POST` | `/peers/:peerId/generateOneTimeLink` | Сгенерировать одноразовый токен для конфига. Ответ: `{ oneTimeLink: "https://..." }`. Токен одноразовый — сбрасывается после первого скачивания. |
| `GET` | `/peers/:peerId/export-json` | Экспорт interconnect-пира как JSON (только interconnect) |

### Скачивание конфига по одноразовой ссылке (публичный)

| Метод | Путь | Auth | Описание |
|-------|------|------|----------|
| `GET` | `/cnf/:token` | ❌ публичный | Скачать WireGuard-конфиг по одноразовому токену (32 hex-символа). Возвращает `.conf` как `text/plain` вложение. Токен аннулируется сразу после скачивания. **404** если токен недействителен или уже использован. |

> Путь `/cnf/*` проксируется Caddy **вне** admin-пути — доступен без знания скрытого URL.

---

## Маршрутизация (Routing)

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/routing/table` | Маршруты ядра. Query: `?table=main` (по умолчанию) |
| `GET` | `/api/routing/tables` | Таблицы маршрутизации из `ip rule show`. Возвращает `{ tables: [...] }` |
| `GET` | `/api/routing/test` | Тест маршрута. Query: `?ip=<dst>[&src=<src>][&mark=<fwmark>]`. С `src`: SimulateTrace (PBR) → `ip route get <dst> mark <fwmark>`. Возвращает `{ result, matchedRule, steps }` |
| `GET` | `/api/routing/routes` | Статические маршруты (из БД). Возвращает `{ routes: [...] }` |
| `POST` | `/api/routing/routes` | Создать маршрут. Body: см. ниже |
| `PATCH` | `/api/routing/routes/:id` | Обновить или переключить: `{ enabled: bool }` |
| `DELETE` | `/api/routing/routes/:id` | Удалить маршрут |

**Структура Route (POST/PATCH body):**

| Поле | Тип | Описание |
|------|-----|----------|
| `destination` | string | CIDR или `"default"` (обязательно) |
| `gateway` | string | Ручной IP шлюза (next-hop). Только для ручного режима |
| `dev` | string | Интерфейс (опционально для ручного режима) |
| `gatewayId` | string | ID шлюза из раздела Gateways — `via`/`dev` берутся из шлюза автоматически |
| `gatewayGroupId` | string | ID группы шлюзов — **автоматический failover** между тирами при падении шлюза |
| `metric` | int | Метрика маршрута (опционально) |
| `table` | string | Таблица маршрутизации (по умолчанию `"main"`) |
| `description` | string | Описание (опционально) |

> `gateway`/`dev` и `gatewayId`/`gatewayGroupId` взаимоисключающие — задайте одно из трёх.
> `gatewayId` и `gatewayGroupId` взаимоисключающие.

**Failover с GatewayGroup:**
Когда маршрут привязан к группе шлюзов (`gatewayGroupId`):
- Нормальная работа: маршрут идёт через шлюз тира 1 (наивысший приоритет)
- При падении тира 1 (статус `"down"` от GatewayMonitor): немедленное переключение на тир 2
- При восстановлении тира 1: возврат к тиру 1 через 30 с (anti-flap)

---

## NAT

### Outbound Source NAT

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/nat/interfaces` | Сетевые интерфейсы хоста. Возвращает `{ interfaces: [...] }` |
| `GET` | `/api/nat/rules` | NAT-правила + авто-правила от интерфейсов. Возвращает `{ rules: [...] }`. Авто-правила имеют `"auto": true` (только чтение) |
| `POST` | `/api/nat/rules` | Создать правило. Body: `{ name, source?, sourceAliasId?, outInterface, type (MASQUERADE/SNAT), toSource? (только SNAT), comment? }` |
| `PATCH` | `/api/nat/rules/:id` | Обновить или переключить: `{ enabled: bool }` |
| `DELETE` | `/api/nat/rules/:id` | Удалить правило |

### Port Forwarding (DNAT)

Перенаправление входящего трафика на другой хост через `iptables-nft PREROUTING DNAT`.
Каждое правило создаёт до 4 iptables-команд на протокол: PREROUTING DNAT + 2× FORWARD ACCEPT + опциональный POSTROUTING MASQUERADE.

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/nat/dnat` | Список DNAT-правил. Возвращает `{ rules: [...] }` |
| `POST` | `/api/nat/dnat` | Создать правило. Body: см. ниже |
| `PATCH` | `/api/nat/dnat/:id` | Обновить или переключить: `{ enabled: bool }` |
| `DELETE` | `/api/nat/dnat/:id` | Удалить правило |

**Структура DnatRule:**

| Поле | Тип | Обязательно | Описание |
|------|-----|-------------|----------|
| `name` | string | ✓ | Название правила |
| `protocol` | string | ✓ | `"tcp"` / `"udp"` / `"both"` |
| `inInterface` | string | | Входящий интерфейс (`"eth0"`, `"ens3"`, …). Пусто = любой |
| `inPort` | int | ✓ | Входящий порт 1–65535 |
| `destIP` | string | ✓ | IP назначения (целевой сервер) |
| `destPort` | int | | Порт назначения 0–65535. `0` = совпадает с `inPort` |
| `masquerade` | bool | | Добавить POSTROUTING MASQUERADE. **Default: `true`**. Нужен когда целевой сервер — публичный хост без маршрута обратно через этот сервер |
| `comment` | string | | Комментарий |
| `enabled` | bool | | Статус (при создании всегда `true`) |

> **Примечание по masquerade:** отключать только если целевой хост подключён через WireGuard-туннель
> в hub-and-spoke топологии, где он и так маршрутизирует ответы обратно через этот сервер.

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
| `POST` | `/api/firewall/rules/:id/move` | Переместить правило на одну позицию. Body: `{ direction: "up"\|"down" }` |
| `POST` | `/api/firewall/reorder` | Переупорядочить все правила разом. Body: `{ ids: ["id1", "id2", ...] }` — полный упорядоченный список всех ID правил, должен содержать ровно текущие ID (без лишних и без пропущенных) |
| `GET` | `/api/firewall/pending` | Отличается ли черновик правил от последнего применённого в ядре снапшота. Возвращает `{ hasPendingChanges: bool }` |
| `POST` | `/api/firewall/apply` | Скопировать черновик → применённый снапшот и пересобрать цепочки iptables. **204** при успехе |
| `POST` | `/api/firewall/discard` | Откатить черновик к последнему применённому снапшоту — без изменений в ядре. **204** при успехе |

> Изменения правил файрвола сохраняются как «черновик» и вступают в силу в ядре только после
> `POST /apply` — после любого create/update/delete/reorder проверяй `GET /pending` и вызывай
> `/apply` (или `/discard`, чтобы отменить), иначе правки останутся неприменёнными.

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
| `GET` | `/api/aliases/client-groups` | Список алиасов типа `client-group` (для выпадающих списков при создании/редактировании пиров). Возвращает `{ groups: [...] }` |
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
| `client-group` | управляется автоматически | Kernel ipset с IP пиров выбранной группы. Обновляется автоматически при создании/изменении/удалении пира. Используется в firewall-правилах для управления трафиком по группам. |
| `group` | `["<aliasId>"]` | Объединяет host/network-алиасы |
| `port` | `["tcp:443", "udp:53", "any:80"]` | L4-порты |
| `port-group` | `["<portAliasId>"]` | Объединяет port-алиасы |

---

## Дашборд (Dashboard)

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/dashboard/widgets` | Сохранённая раскладка виджетов текущего пользователя. Query: `?page=dashboard` (по умолчанию) или `?page=diagnostics`. Возвращает `{ "widgets": [...] }` — форма массива определяется фронтендом, для API это непрозрачные данные |
| `PUT` | `/api/dashboard/widgets` | Сохранить раскладку виджетов. Тот же query `?page=`. Body: `{ "widgets": [...] }` |
| `GET` | `/api/dashboard/system-info` | Метрики хоста для карточки System Info на дашборде |

**GET /api/dashboard/system-info — поля ответа:**

| Поле | Тип | Описание |
|------|-----|----------|
| `hostname` | string | |
| `uptime` | string | Человекочитаемый формат, например `"3d 4h 12m"` |
| `uptimeSec` | int | |
| `load1`, `load5`, `load15` | float | `/proc/loadavg` |
| `memTotal`, `memFree`, `memUsed` | int | кБ. `memFree` использует `MemAvailable`, если доступен |
| `memPct` | int | 0–100 |
| `awgCliVersion` | string | Только kernel-режим. `""` если не определилось |
| `awgKernelVersion` | string | Только kernel-режим. `""` если не определилось или userspace-режим |
| `awgVersionMismatch` | bool | `true`, если major.minor версии AWG CLI и загруженного kernel-модуля различаются — см. [Диагностику проблем](../README.ru.md#️-диагностика-проблем) |

> Каждая сохранённая строка виджета, ссылающаяся на уже удалённый шлюз (`"gateway:<id>"` в
> `graphs`/`graphColors`), тихо вычищается при следующем `GET /widgets` для этого
> пользователя/страницы — самовосстановление, отдельный эндпоинт очистки не нужен.

---

## Метрики (Metrics)

Метрики системы/шлюзов в реальном времени и история, на основе внутреннего сэмплера и истории в SQLite.

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/metrics` | Текущий снапшот: CPU, RAM, пропускная способность по интерфейсам, статус по шлюзам. Возвращает `{ cpu, mem, memUsedMb, memTotalMb, net: {iface: {rxMbps, txMbps}}, interfaces: [...], gateways: {id: status} }` |
| `GET` | `/api/metrics/history` | Исторические точки по одному ключу метрики. Query: `?key=cpu&period=5m\|1h\|6h\|24h\|7d\|30d` (по умолчанию `5m`). Возвращает `{ key, period, points: [[timestamp, value], ...] }` |
| `GET` | `/api/metrics/gateway-dist` | Распределение статусов шлюза по бакетам — для столбчатой диаграммы на странице Diagnostics. Query: `?key=gateway:<id>&period=1h` (`key` должен начинаться с `gateway:`). Возвращает `{ key, period, buckets: [[ts_ms, healthyCount, degradedCount, downCount, adminDownCount], ...] }` |

`period` определяет и глубину истории, и размер бакета: `5m`→5с, `1h`→60с, `6h`→300с, `24h`→900с,
`7d`→3600с, `30d`→21600с.

---

## Диагностика (Diagnostics)

Инструменты сетевой диагностики по запросу, выполняются на сервере. Стриминговые эндпоинты
используют Server-Sent Events (SSE) — каждая строка live-вывода команды приходит как одно
событие `data: <line>\n\n`; поток завершается событием `data: [done]\n\n`.

| Метод | Путь | Описание |
|-------|------|----------|
| `POST` | `/api/diagnostics/ping` | Разовый ping, JSON-результат (не стрим). Body: `{ host, count? }` (count 1–10, по умолчанию 3). Возвращает `{ reachable, latencyMs, packetLoss }` |
| `GET` | `/api/diagnostics/ping/stream` | Потоковый ping (SSE), построчный терминальный вывод. Query: `?host=...&count=1-20(по умолчанию 5)&source=<iface>&size=<байты 0-65507>&df=true&tos=0-255` |
| `GET` | `/api/diagnostics/traceroute/stream` | Потоковый traceroute (SSE). Query: `?host=...&type=udp(по умолчанию)\|icmp\|tcp&source=<src IP>` |
| `GET` | `/api/diagnostics/tcpdump/stream` | Потоковый захват пакетов (SSE). Query: `?iface=<name>&filter=<BPF-выражение>&save=true` |
| `POST` | `/api/diagnostics/tcpdump/stop` | Остановить захват с `save=true` и финализировать PCAP-файл. Query: `?file=<captureId>` (из события `[captureid:<id>]` потока) |
| `GET` | `/api/diagnostics/tcpdump/download` | Скачать финализированный PCAP-файл, затем удалить его с сервера. Query: `?file=<captureId>` |

**Флоу сохранения tcpdump:** запусти `GET /tcpdump/stream?iface=wg10&save=true`; *первое* SSE-событие —
`[captureid:<hex-id>]` — сохрани его сразу. Вызови `POST /tcpdump/stop?file=<id>`, чтобы послать
`SIGINT` и сбросить файл на диск, затем `GET /tcpdump/download?file=<id>`, чтобы скачать (одноразово —
файл и запись о нём удаляются после скачивания, а также по истечении жёсткого таймаута 300 с без вызова stop).

`host`/`source`/`iface` валидируются на сервере строгим набором символов (буквы/цифры/`.`/`-`/`_`/`:`/`[`/`]`)
— shell-метасимволы не принимаются.

---

## Удалённые серверы / Мультисервер (Remotes)

Позволяет одному инстансу Cascade управлять другими: браузер общается только с локальным
сервером, который проксирует аутентифицированные запросы к зарегистрированным remote-серверам,
используя сохранённый API-токен. Используется для сайдбара мультисервера и кросс-серверного
Speed Test.

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/remotes` | Список зарегистрированных remote-серверов. Возвращает `{ remotes: [...] }` (токены никогда не включаются в ответ) |
| `POST` | `/api/remotes` | Зарегистрировать remote. Два режима — см. ниже |
| `DELETE` | `/api/remotes/:id` | Удалить remote |
| `POST` | `/api/remotes/:id/test` | Проверка связи (пингует remote его сохранённым токеном). Возвращает `{ ok: true }` или ошибку **502** |
| `ALL` | `/api/remotes/:id/proxy/*` | Проксирует запрос на `/api/*` remote-сервера, подставляя его сохранённый Bearer-токен. Браузер никогда не видит учётные данные remote-сервера |

**POST /api/remotes — режим логина:**

| Поле | Тип | Описание |
|------|-----|----------|
| `name` | string | Отображаемое имя (обязательно) |
| `url` | string | Базовый URL remote-сервера — должен резолвиться в публичный адрес (SSRF-защита) (обязательно) |
| `username`, `password` | string | Админ-креды remote-сервера, используются один раз для получения токена |
| `totpCode` | string | Нужен, только если на remote включена 2FA — см. ниже |
| `skipTlsVerify` | bool | Пропустить проверку TLS-сертификата (для self-signed) |

Если на remote включена TOTP и `totpCode` не передан, ответ — **422** `{ "totp_required": true }` —
повтори тот же запрос с заполненным `totpCode`.

**POST /api/remotes — режим явного токена:** передай `token` вместо `username`/`password`, чтобы
зарегистрировать уже существующий API-токен напрямую (валидируется пингом remote-сервера перед
сохранением) — логин не выполняется, 2FA-флоу не задействуется.

> `proxyRemote` вырезает `Authorization`/`Cookie`/`Host` из пересылаемого запроса и не пересылает
> `Set-Cookie` из ответа remote-сервера — так сессия remote-сервера никогда не сможет утечь в
> локальную сессию браузера или перезаписать её. Редиректы обрабатываются с той же SSRF-проверкой
> на каждом хопе.

---

## Speed Test

Тест пропускной способности `iperf3` по запросу между любыми двумя серверами Cascade (или локальным
сервером и произвольным хостом). Эндпоинты `run`/`result*` — те, что напрямую вызывает UI; `server`/
`client` — внутренние эндпоинты оркестрации (доступны напрямую, если пишешь свой скрипт теста),
которые сервер-*источник* вызывает на сервере-*назначении* через прокси Remotes.

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/speedtest/check` | Установлен ли `iperf3` на этом сервере. Возвращает `{ installed: bool, path? }` |
| `POST` | `/api/speedtest/run` | Запустить асинхронный тест. Body: см. ниже. Возвращает **201** `{ jobId }` сразу — тест выполняется в фоне |
| `GET` | `/api/speedtest/result/:jobId` | Опросить задачу. Возвращает `SpeedtestRecord` (см. ниже); `status` — `"running"`, `"done"` или `"error"` |
| `GET` | `/api/speedtest/results` | Вся история (последние 100 запусков). Возвращает `{ results: [SpeedtestRecord, ...] }` |
| `DELETE` | `/api/speedtest/results` | Очистить историю |
| `POST` | `/api/speedtest/server` | *Внутренний.* Запустить `iperf3 -s --one-off` на этом сервере. Возвращает `{ port, sessionId }` |
| `DELETE` | `/api/speedtest/server/:sessionId` | *Внутренний.* Убить работающую сессию `iperf3`-сервера |
| `POST` | `/api/speedtest/client` | *Внутренний.* Запустить `iperf3 -c` против заданного host/port и вернуть результат |

**POST /api/speedtest/run — body:**

| Поле | Тип | Описание |
|------|-----|----------|
| `fromServer`, `toServer` | string | Отображаемые имена, сохраняются вместе с результатом для читаемости истории |
| `fromRemoteId`, `toRemoteId` | string | `""` = локальный сервер, иначе ID remote-сервера — определяет, какая сторона запускает `iperf3 -s`, а какая `-c` |
| `host` | string | IP/хост, к которому подключается клиент `iperf3` (обязательно) |
| `bindAddr` | string | Опционально: привязать клиента к конкретному локальному IP (например, IP туннельного интерфейса — для теста в режиме tunnel) |
| `via` | string | `"tunnel"` или `"internet"` — только информационное поле, сохраняется с результатом; сам тест не меняет, кроме `host`/`bindAddr` |
| `duration` | int | Секунды, по умолчанию 10 |
| `streams` | int | Параллельные TCP-потоки (`iperf3 -P`), по умолчанию 4 |

**Поля SpeedtestRecord:** `id, fromServer, toServer, host, port, duration, streams, status, via,
sendMbps, recvMbps, retransmits, latencyMs, error, startedAt, finishedAt`. Поля `Mbps`/
`retransmits`/`latencyMs` — `null`, пока `status` не станет `"done"`.

> Тест всегда идёт как чистый TCP (`iperf3` без `-u`), независимо от `via` — при `via: "tunnel"`
> инкапсуляция WireGuard/AmneziaWG в UDP всё равно применяется снизу, поэтому троттлинг UDP на
> стороне провайдера может дать результат в tunnel-режиме сильно ниже, чем в internet-режиме,
> на абсолютно той же паре серверов. Это реальный эффект сетевого пути, не баг — см. заметку про
> диагностику в README, если увидишь неожиданно большой разрыв между двумя режимами.

---

## Системный бэкап

### Создать бэкап

```
POST /api/system/backup
Content-Type: application/json
Authorization: Bearer ws_...

{ "password": "optional" }
```

| Поле | Тип | Описание |
|------|-----|----------|
| `password` | string | Опционально. Если указан — файл шифруется AES-256-GCM. Пустая строка или отсутствие поля — без шифрования. |

**Ответ:** бинарный поток (файл для скачивания).

| Пароль | Имя файла | Content-Type |
|--------|-----------|--------------|
| Не указан | `cascade-backup-YYYYMMDD-HHMMSS.tar.gz` | `application/gzip` |
| Указан | `cascade-backup-YYYYMMDD-HHMMSS.tar.gz.enc` | `application/octet-stream` |

Содержимое архива: `awg.db` + `*.save` (ipset файлы).

**Примеры (curl):**

```bash
# Без пароля
curl -X POST https://<host>/<admin_path>/api/system/backup \
  -H "Authorization: Bearer ws_..." \
  -H "Content-Type: application/json" \
  -d '{}' \
  -o cascade-backup.tar.gz

# С паролем (зашифрованный)
curl -X POST https://<host>/<admin_path>/api/system/backup \
  -H "Authorization: Bearer ws_..." \
  -H "Content-Type: application/json" \
  -d '{"password": "mypassword"}' \
  -o cascade-backup.tar.gz.enc
```

### Предпросмотр восстановления

```
POST /api/system/restore/preview
Content-Type: multipart/form-data
Authorization: Bearer ws_...
```

| Поле | Тип | Описание |
|------|-----|----------|
| `backup` | file | `.tar.gz` или `.tar.gz.enc` файл бэкапа |
| `password` | string | Обязателен если файл зашифрован, иначе — `400` |

Читает БД из бэкапа (не трогая текущее состояние) и сравнивает имена физических интерфейсов,
на которые ссылаются его NAT-правила, с реальными интерфейсами этого сервера — полезно при
восстановлении бэкапа, снятого на другой машине, где `eth0`/`ens3`/и т.д. могут не совпадать.

**Ответ (200):** `{ "backupIfaces": [...], "serverIfaces": [...], "needsRemap": bool }`

Если `needsRemap: true`, передай `ifaceMap` (например `{"eth0":"ens3"}`) в `POST /restore` ниже,
чтобы переписать `out_interface` в восстановленных NAT-правилах.

### Восстановить из бэкапа

```
POST /api/system/restore
Content-Type: multipart/form-data
Authorization: Bearer ws_...
```

| Поле | Тип | Описание |
|------|-----|----------|
| `backup` | file | `.tar.gz` или `.tar.gz.enc` файл бэкапа |
| `password` | string | Обязателен если файл зашифрован, иначе — `400` |
| `ifaceMap` | string (JSON) | Опционально. Например `{"eth0":"ens3"}` — переписывает `out_interface` в восстановленной таблице `nat_rules`. См. «Предпросмотр восстановления» выше |

**Ответ (200):** `{ "message": "Backup restored. Container is restarting…", "restored": N }`

Процесс восстановления: сначала автоматически бэкапит текущее состояние в
`data/pre-restore-<timestamp>.tar.gz`, останавливает все WireGuard-интерфейсы, сбрасывает цепочки
файрвола и ipset'ы, записывает файлы из бэкапа поверх текущей директории данных, удаляет старые
WAL/SHM-файлы, применяет `ifaceMap` (если передан), затем завершает процесс — Docker
(`restart: always`) поднимает контейнер заново уже с восстановленным состоянием.

**Ошибки:**
- `400 "this backup is encrypted — provide the password"` — зашифрованный файл без пароля
- `400 "wrong password or corrupted backup file"` — неверный пароль (данные не тронуты)

После успешного восстановления процесс завершается через 300 мс — Docker перезапускает контейнер (`restart: always`).

**Примеры (curl):**

```bash
# Незашифрованный
curl -X POST https://<host>/<admin_path>/api/system/restore \
  -H "Authorization: Bearer ws_..." \
  -F "backup=@cascade-backup.tar.gz"

# Зашифрованный
curl -X POST https://<host>/<admin_path>/api/system/restore \
  -H "Authorization: Bearer ws_..." \
  -F "backup=@cascade-backup.tar.gz.enc" \
  -F "password=mypassword"
```

### Список авто-бэкапов перед восстановлением

```
GET /api/system/backups
```

Каждый `POST /restore` автоматически снимает снапшот текущего состояния в
`data/pre-restore-<timestamp>.tar.gz` перед тем, как что-либо перезаписать (см. выше) — этот
эндпоинт выводит список таких защитных снапшотов.

**Ответ (200):** `{ "backups": [{ "name": "pre-restore-20260115-030405.tar.gz", "size": 123456, "createdAt": "2026-01-15T03:04:05Z" }, ...] }`

Эти файлы **нельзя** скачать через API — при необходимости восстанови вручную из директории
данных сервера (`docker exec cascade ls /etc/wireguard/data/`).

### Автоматический бэкап (cron)

```bash
#!/bin/bash
# /etc/cron.daily/cascade-backup
DATE=$(date +%Y%m%d-%H%M%S)
DEST="/var/backups/cascade"
mkdir -p "$DEST"

curl -sf -X POST https://<host>/<admin_path>/api/system/backup \
  -H "Authorization: Bearer ws_..." \
  -H "Content-Type: application/json" \
  -d '{"password": "your-backup-password"}' \
  -o "$DEST/cascade-$DATE.tar.gz.enc"

# Удалить бэкапы старше 30 дней
find "$DEST" -name "*.tar.gz.enc" -mtime +30 -delete
```

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
| `GET` | `/api/wg-enable-one-time-links` | `true` |
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
