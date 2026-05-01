# Cascade — Claude Memory

## ⚠️ ПРАВИЛО №0: ЧИТАТЬ ЭТОТ ФАЙЛ ПЕРЕД ЛЮБЫМ ДЕЙСТВИЕМ

Перед тем как вносить **любые изменения** (код, конфиг, документация, коммит):
1. Прочитать этот файл (`CLAUDE.md`) полностью через Read tool
2. Явно написать пользователю: **«CLAUDE.md прочитан. Правила уяснены: [перечислить применимые к текущей задаче]»**
3. Только после этого — приступать

Без этих двух шагов никакие изменения не начинаются.

# Название проекта:
Cascade

# Описание:
Self-hosted веб-интерфейс для управления WireGuard и AmneziaWG VPN роутером.
Поддерживает несколько туннельных интерфейсов, peer management, политики маршрутизации,
NAT, файрвол, мониторинг шлюзов и S2S interconnect между роутерами.

# Архитектура:

- **Frontend**: Vue 2 (CDN), Tailwind CSS (прекомпилированный статический файл), ApexCharts, VueI18n. Встроен в бинарник через `go:embed`.
- **Backend**: Go 1.23, Fiber v2, AmneziaWG / WireGuard (`awg-quick` / `wg-quick`), `iptables-nft`, `iproute2`. Docker `--network host`.
- **База данных**: SQLite (`modernc.org/sqlite` — pure Go, без CGO). Файл: `/etc/wireguard/data/cascade.db`.

---

## CRITICAL RULES
- NEVER удалять или переписывать рабочий код без явного запроса
- NEVER удалять файлы без подтверждения
- ALWAYS запускать тесты после любого изменения кода
- ALWAYS делать git checkpoint перед крупными рефакторингами
- Одна задача за раз. НЕ делать несколько изменений одновременно
- Если не уверен — СПРОСИ, не угадывай

## ⚠️ ПРАВИЛО №3: АГЕНТЫ — ТОЛЬКО АНАЛИЗ, НИКАКИХ АВТОДЕЙСТВИЙ

После запуска любого агента (code-reviewer, tester, planner) — **ОСТАНОВИТЬСЯ**.
Показать результат пользователю и ждать явной команды «делай» / «фиксим X».

**Запрещено без явного аппрувала пользователя:**
- Начинать исправлять код после code-reviewer
- Начинать писать тесты после tester
- Начинать реализацию после planner

Результат агента = информация для пользователя. Не инструкция для Claude.

## ⚠️ ПРАВИЛО №4: ОБЯЗАТЕЛЬНЫЙ ПОРЯДОК РАБОТЫ НАД КОДОМ

**Каждый шаг требует явного аппрувала пользователя перед следующим:**

0. **Обсуждение и план** → показать план → ждать «делай»
1. **Git checkpoint** — перед крупными изменениями:
   ```bash
   git tag checkpoint/<описание> && git push origin checkpoint/<описание>
   ```
2. **Реализация** — написать код
3. **Тесты** — запустить агента `tester` → показать результат → ждать команды
   - написать тесты для нового кода (каждая фича покрыта)
   - запустить полный suite (`go test ./internal/... -count=1 -timeout 120s`)
   - все тесты зелёные
4. **Code Review** — запустить агента `code-reviewer` → показать результат → ждать команды
   - не исправлять автоматически — только показать findings
5. **Коммит** — только при зелёных тестах + явном аппрувале пользователя

**Нарушение любого шага = остановиться и спросить пользователя.**
**Коммит без прохождения шагов 3 и 4 — ЗАПРЕЩЁН.**

## Working Style
- Сначала ПЛАН, потом код — никогда наоборот
- Маленькие дифы: один файл → тесты → следующий файл
- Используй субагентов для исследования кодовой базы

## Agents
- Use `planner` agent для планирования перед крупными изменениями
- Use `Explore` agent для исследования кодовой базы
- Use `tester` agent после изменений кода — результат показать, не действовать
- Use `code-reviewer` agent перед коммитами — результат показать, не действовать

---

## ⚠️ ПРАВИЛО №1: ПЕРЕД РЕДАКТИРОВАНИЕМ ЛЮБОГО ФАЙЛА

**ВСЕГДА читать файл целиком через Read tool, ТОЛЬКО ПОТОМ делать точечное изменение через Edit.**
Никогда не писать код "из головы" или "по памяти" — только читать → редактировать.

## ⚠️ ПРАВИЛО №2: TAILWIND CSS — ТОЛЬКО СУЩЕСТВУЮЩИЕ КЛАССЫ

`internal/frontend/www/css/app.css` — прекомпилированный статический файл. Новые Tailwind-классы **не работают**.
Перед использованием любого класса: `grep "класс" internal/frontend/www/css/app.css`
Если класса нет — использовать `style="..."` (inline CSS).

Отсутствующие классы: `px-6`, `py-10`, `py-8`, `p-6`, `border-t`, `border-neutral-*`, `space-y-2`, `space-y-4`, `space-y-6`, `hover:bg-*` → inline style.

**Паттерн для модалок:**
```html
<div v-if="showModal" class="fixed inset-0 bg-black bg-opacity-50 z-50 overflow-y-auto"
  style="padding:40px 24px;" @click.self="showModal = false">
  <div class="bg-white dark:bg-neutral-700 rounded-lg" style="max-width:520px; margin:0 auto;">
    <!-- контент -->
  </div>
</div>
```

**Динамические цвета** — использовать `:style` с `theme` computed (возвращает `'dark'`/`'light'`, учитывает `auto`):
```html
:style="{ color: theme === 'dark' ? '#f5f5f5' : '#1f2937' }"
```

**Vue 2 — обновление массива только через splice:**
```javascript
this.tunnelInterfaces.splice(idx, 1, updatedIface); // НЕ array[idx] = item
```

---

## Правила работы

- Репо: `git@github.com:JohnnyVBut/cascade.git`
- Текущая ветка: **`master`** — Go rewrite, продакшн-готовая.
- Разработка фич ведётся в `feature/...` ветках, merge в `master` через явный запрос пользователя.
- После каждого пуша напоминать команды деплоя на сервере
- Каждый коммит — подробное сообщение (что, почему, какие файлы)

## Деплой на сервере

```bash
cd /root/cascade
git pull origin master
./build-go.sh
docker compose -f docker-compose.go.yml down && docker compose -f docker-compose.go.yml up -d
```

---

## Ключевые файлы (Go)

| Файл | Роль |
|------|------|
| `cmd/awg-easy/main.go` | Точка входа, инициализация |
| `internal/api/` | HTTP handlers (Fiber) |
| `internal/tunnel/` | WG/AWG интерфейсы, peers |
| `internal/firewall/` | iptables-nft, PBR цепочки |
| `internal/routing/` | ip route, static routes |
| `internal/nat/` | NAT правила |
| `internal/gateway/` | Мониторинг шлюзов |
| `internal/aliases/` | Алиасы + ipset |
| `internal/settings/` | Глобальные настройки (SQLite) |
| `internal/db/db.go` | SQLite инициализация, миграции |
| `internal/api/compat.go` | Заглушки для старых Node.js эндпоинтов |
| `internal/frontend/www/` | Фронтенд (Vue 2 + Tailwind) |
| `internal/frontend/www/js/app.js` | Vue app: data + methods |
| `internal/frontend/www/js/api.js` | Клиентские API методы |
| `internal/frontend/www/index.html` | Весь UI (один файл) |

---

## Архитектурные решения (критично знать)

| Решение | Детали |
|---------|--------|
| `embed.FS` | Фронтенд вшит в бинарник при сборке → **нужен `./build.sh` после любого изменения фронтенда** |
| Middleware порядок | `recover → logging → api routes → static(SPA fallback)`. Static ПОСЛЕ api — иначе глотает API запросы |
| Nil slice → JSON null | Go `nil` slice → `null` в JSON → TypeError на фронте. Всегда инициализировать `[]Type{}` перед `c.JSON()` |
| Логирование | Только мутации (POST/PATCH/DELETE/PUT) + ошибки (4xx/5xx). GET 200 не логируются — polling каждую секунду |
| `ip -j` ЗАПРЕЩЕНО | JSON флаг `ip` зависает навсегда на некоторых ядрах Linux. Только текстовый вывод + парсинг |
| AWG syncconf | `awg set peer` вызывает deadlock в kernel module. Всегда: `syncconf` (reload) или `Restart()` |
| FW init order | `firewall.RebuildChains()` вызывать ПОСЛЕ `tunnel.Init()` — иначе `ip route` падает (интерфейс не существует) |

---

## API contract — обёртка ответов

Фронтенд использует `res.key || []`. Go ОБЯЗАН оборачивать:

| Эндпоинт | Ключ |
|----------|------|
| GET /tunnel-interfaces | `{ interfaces: [...] }` |
| GET .../peers | `{ peers: [...] }` |
| POST .../peers | `{ peer: {...} }` |
| POST .../peers/import-json | `{ peer: {...} }` |
| GET /routing/table | `{ routes: [...] }` |
| GET /routing/tables | `{ tables: [...] }` |
| GET /routing/routes | `{ routes: [...] }` |
| GET /routing/test | `{ result, matchedRule: null, steps: [] }` |
| GET /nat/interfaces | `{ interfaces: [...] }` |
| GET /nat/rules | `{ rules: [...] }` |
| GET /gateways | `{ gateways: [...] }` |
| GET /gateway-groups | `{ groups: [...] }` |

---

## Go-специфичные фиксы (FIX-GO)

**FIX-GO-1: nil slice → JSON null**
`nil` slice → `null` в JSON → TypeError. Всегда `[]Type{}` перед `c.JSON()`.

**FIX-GO-2: Static middleware после API routes**
`filesystem.New()` до `/api/*` → все API запросы отдают `index.html`. Static — последним.

**FIX-GO-3: Alias generation race condition**
`getAliasJobStatus` вызывает `FinalizeGeneration()` синхронно перед ответом — иначе фронтенд видит `entryCount=0`.

**FIX-GO-6: /api/release → 999999**
Старый фронтенд сравнивает версию с changelog. `0 < 14` → баннер "Update available". Фикс: `999999`.

**FIX-GO-8: Route test — SimulateTrace + fwmark вместо `from`**
`ip route get <dst> from <src>` → "Network unreachable" если src не локальный.
Правильно: `SimulateTrace(src, dst)` → fwmark → `ip route get <dst> mark <fwmark>`.

**FIX-GO-9: PBR routing table пустая после рестарта контейнера**
`firewall.RebuildChains()` должен вызываться ПОСЛЕ `tunnel.Init()`.
В `startInterface` и `restartInterface`: вызывать `firewall.Get().RebuildChains()`.
Использовать `onlink` во всех `ip route replace` (шлюз может быть вне подсети).

**FIX-GO-10: ipInCIDR всегда false**
Bitmask ротация — неверный алгоритм. Использовать `net.ParseCIDR(cidr)` + `ipNet.Contains(ip)`.

**FIX-GO-11: importPeerJSON не сохраняет address**
Явно читать `body["address"]` в `inp.Address` до обработки `allowedIPs`.

**FIX-GO-12: Фронтенд в `internal/frontend/www/`, не `src/www/`**
`src/www/` — Node.js версия, не попадает в Go-бинарник.

**FIX-GO-13: GetAllPeers — map iteration non-deterministic**
Go рандомизирует порядок map → пиры перемешиваются каждую секунду.
Фикс: `sort.Slice` по `CreatedAt` в `GetAllPeers()`.

**FIX-GO-16: doReload() — 10s timeout + fallback на Restart()**
`awg syncconf` может зависнуть (AWG kernel deadlock). Timeout 10s + fallback на `Restart()`.

---

## Compat layer (`internal/api/compat.go`)

**RegisterCompat** (без авторизации): `/lang → "en"`, `/release → 999999`, `/remember-me → true`, `/ui-traffic-stats → true`, `/ui-chart-type → 1`, `/wg-enable-one-time-links → false`, `/ui-sort-clients → false`, `/wg-enable-expire-time → false`, `/ui-avatar-settings → {dicebear:null}`

**RegisterCompatAuth** (требует auth): `GET /wireguard/client → []`, `ALL /wireguard/* → 501`, `GET /system/interfaces → {interfaces:[...]}`

---

## Checkpoint (master — текущее состояние)

**Последний коммит:** `8ca674a` fix(peers): use AllowedIPs mask for peer address, not iface mask

**Что работает (полностью реализовано и протестировано):**

| Область | Детали |
|---------|--------|
| **Interfaces** | CRUD, start/stop/restart, quick-create (one-click), export-params, backup/restore |
| **Peers** | CRUD, client/S2S, auto-allocate IP (/32), QR, config download, enable/disable, name/address/expireDate, export-json, one-time links (endpoint готов, UI кнопка скрыта feature flag) |
| **S2S Interconnect** | export-params → import-json, PSK sync, address из JSON |
| **Routing** | Static routes CRUD, kernel routes (ip route show), routing tables (ip rule show), SimulateTrace (PBR trace), route test |
| **NAT Outbound** | SNAT/MASQUERADE CRUD + alias source + auto-правила от интерфейсов |
| **NAT Port Forwarding** | DNAT CRUD — протокол tcp/udp/both, inbound port, dest IP/port, masquerade toggle |
| **Gateways** | CRUD + live ping/HTTP monitoring + Gateway Groups + fallback (blackhole/default, 30s anti-flap) |
| **Firewall Aliases** | 6 типов: host/network/ipset/group/port/port-group; upload; async generate (RIPE) |
| **Firewall Rules** | ACCEPT/DROP/REJECT + PBR (gateway) + port alias matching + ↑↓ order + default policy (accept/drop) |
| **AWG2 Templates** | CRUD + Generate (11 профилей: QUIC, TLS 1.3, DTLS, HTTP/3, SIP, Noise_IK, dns_query + др.) |
| **Auth** | Multi-user, bcrypt (cost 12), TOTP 2FA (RFC 6238, Google Authenticator), API tokens (Bearer ws_...), sessions (HttpOnly, SameSite=Strict), rate limit 5/min, 500ms delay |
| **Traffic stats** | persistent total_rx/total_tx в SQLite, flush каждые 60s + при graceful shutdown |
| **Settings** | Global config: dns, keepalive, clientAllowedIPs, subnetPool, portPool, defaultFwPolicy, gateway thresholds, publicIP override |
| **Deploy** | setup.sh: swap, kernel, Docker, AWG userspace/kernel, TLS (acme.sh shortlived), Caddy (HTTPS/HTTP3, hidden admin path, decoy site) |
| **Peer address** | Всегда /32 для клиентских пиров (маска берётся из AllowedIPs, не из интерфейса) |

**Что НЕ реализовано:**
- **Admin Tunnel (wg0)** — заглушка: `GET /api/wireguard/client → []`, все мутации → 501
- **acme.sh cron** — `--install-cronjob` не вызывается в setup.sh; 6-дневный сертификат истечёт без ручной установки cron

**Хотелки (обсуждались, не запланированы):**
- INPUT chain в FirewallManager (управление доступом к серверу из UI)
- Multi-user RBAC (superadmin / operator / viewer с resource-scoped доступом)
- Telegram Bot для управления роутером
- Ethernet-интерфейсы в dashboard (live TX/RX из /proc/net/dev)

**S2S топология — ограничения:**
- `allowedIPs=0.0.0.0/0` работает только для **одного** пира — при 2+ пирах WG выберет один
- Full mesh: каждый пир — `/32` соседа + нужные префиксы
- Рекомендуется: hub-and-spoke или full mesh с явными prefix-листами
