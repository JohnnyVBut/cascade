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
| `client-group` | управляется автоматически | Kernel ipset с IP пиров выбранной группы. Обновляется автоматически при создании/изменении/удалении пира. Используется в firewall-правилах для управления трафиком по группам. |
| `group` | `["<aliasId>"]` | Объединяет host/network-алиасы |
| `port` | `["tcp:443", "udp:53", "any:80"]` | L4-порты |
| `port-group` | `["<portAliasId>"]` | Объединяет port-алиасы |

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

**Ответ (200):** `{ "message": "Backup restored. Container is restarting…", "restored": N }`

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
