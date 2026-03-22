# AWG-Easy 2.0 — Requirements

> Статус: **реализация в процессе** | Последнее обновление: 2026-03-12 (NAT page: Outbound Source NAT CRUD)
> Ветка: `feature/kernel-module` | Репо: `git@github.com:JohnnyVBut/cascade.git`

---

## Статус реализации

### ✅ Готово

#### Backend
| Файл | Что сделано |
|------|------------|
| `src/lib/TunnelInterface.js` | Управление одним WG/AWG интерфейсом: генерация конфига, start/stop/restart, down→up при "already exists", reload (syncconf) |
| `src/lib/InterfaceManager.js` | Singleton. Хранит все data-plane интерфейсы (Map). Авто-старт при перезапуске контейнера. CRUD + peer management |
| `src/lib/Peer.js` | Модель пира. Генерация wg-конфига `[Peer]`. Генерация клиентского конфига (полный + шаблон). QR-ready |
| `src/lib/Settings.js` | **НОВОЕ.** Singleton. Хранит глобальные настройки (dns, keepalive, allowedIPs) и AWG2 Templates в `/etc/wireguard/data/settings.json`. `applyTemplate()` возвращает параметры с рандомизированными H1-H4 |
| `src/lib/Server.js` | API маршруты: tunnel-interfaces CRUD + start/stop/restart + peers CRUD + config/QR. **+ Settings API** (GET/PUT `/api/settings`) **+ Templates API** (CRUD + set-default + apply) |
| `src/www/js/api.js` | Клиентские методы: settings, templates, tunnel-interfaces, peers |
| `src/lib/WireGuard.js` | Старый wg0 интерфейс (для вкладки Clients — оставлен без изменений) |
| PostUp/PostDown | `iptables-nft` с FORWARD ACCEPT + MASQUERADE. `Table = off` если `disableRoutes` |
| Хранилище | Интерфейсы: `/etc/wireguard/data/interfaces/{id}.json`. Пиры: `/etc/wireguard/data/peers/{id}/{peerId}.json`. Настройки: `/etc/wireguard/data/settings.json` |

#### UI (sidebar навигация)
| Страница | Ключ `activePage` | Статус | Примечание |
|----------|------------------|--------|-----------|
| **Interfaces** | `'interfaces'` | ✅ Работает | Динамические вкладки, per-interface view (info + peers) |
| **Gateways** | `'gateways'` | ⏳ Placeholder | "Coming soon" |
| **Routing** | `'routing'` | ✅ Работает | Status (kernel routes + route test) + Static CRUD + OSPF placeholder |
| **NAT** | `'nat'` | ✅ Работает | Outbound NAT CRUD + toggle; Port Forwarding placeholder |
| **Firewall** | `'firewall'` | ⏳ Placeholder | "Coming soon" |
| **Settings** | `'settings'` | ✅ Работает | Global Settings + AWG2 Templates |
| **Administration** | `'administration'` | ✅ Работает | Admin Tunnel (бывший Clients tab) |

*WAN Tunnels* — полностью удалены (код, HTML, данные, методы).
*Горизонтальные табы* — заменены боковым меню (sidebar).

---

### 🚧 Не реализовано (очерёдность)

#### Следующий шаг — Admin Instance (backend + UI)
- [ ] `src/lib/AdminInstance.js` — отдельный класс. Параметры берёт из env vars (`WG_ADMIN_ADDRESS`, `WG_PORT`, `JC`, `JMIN` и т.д.). Ключи генерирует при первом старте, сохраняет в `/etc/wireguard/data/admin.json`. Поднимается автоматически. Управление через UI: только добавление/удаление пиров
- [ ] API: `GET /api/admin` (статус), `GET/POST/DELETE /api/admin/peers` (пиры), `GET /api/admin/peers/:id/config`, `GET /api/admin/peers/:id/qrcode.svg`
- [ ] UI: вкладка **Admin** — статус интерфейса (read-only), список пиров с QR/download/delete

#### Instances Tab (рефакторинг существующей Tunnel Interfaces)
- [ ] **Edit modal** для интерфейса (все поля кроме privateKey). После сохранения — сообщение "Stop & Start to apply"
- [ ] **Disable Routes** checkbox в форме create/edit → `Table = off` в конфиге
- [ ] **Private key** как password-поле при создании (скрыт символами)
- [ ] **Public key** с кнопкой Copy
- [ ] **Load from template** dropdown в AWG2-форме — вызывает `POST /api/templates/:id/apply`, подставляет результат + рандомизирует H1-H4
- [ ] Автозаполнение AWG2 из дефолтного шаблона при переключении на протокол AWG2

#### Peers Tab (отдельная вкладка)
- [ ] Отдельная вкладка **Peers** (сейчас peers встроены в Tunnel Interfaces)
- [ ] Dropdown фильтр "All / конкретный Instance" в шапке
- [ ] Enable/disable toggle per peer (persisted в JSON, disabled peer → не в конфиг)
- [ ] Индикатор online/offline (handshake < 3 мин → online)
- [ ] RX/TX трафик из `wg show` / `awg show` (polling)
- [ ] Edit modal для пира
- [ ] Backend: `GET /api/peers` (все пиры всех интерфейсов, с фильтром `?interfaceId=`)
- [ ] Backend: `GET /api/tunnel-interfaces/:id/peers/stats` (handshake + трафик)

#### Прочее
- [ ] Автозаполнение Address пира (следующий свободный IP в подсети инстанса)
- [ ] Валидация адреса пира (в подсети, не совпадает с адресом интерфейса / другими пирами)
- [ ] Pre-shared key — авто-генерация при создании пира (уже генерируется в TunnelInterface.js, нужно убедиться что отображается в UI как "скрытый с кнопкой Copy")

---

### Технические решения (зафиксировано)

- `iptables-nft` везде (не `iptables`) — Ubuntu 22.04 + nftables backend
- При "already exists" на restart контейнера — down→up цикл
- H1-H4 хранятся как диапазоны `start-end`, рандомизируются при каждом apply шаблона
- Приватный ключ хранится на сервере (нужен для QR/download)
- `--network host` — интерфейсы живут в ядре хоста между рестартами контейнера
- Маска пира всегда `/32`, подсеть только для крипторутинга
- **`ip -j` (JSON флаг) ЗАПРЕЩЁН** — зависает навсегда на некоторых конфигурациях ядра Linux.
  Везде использовать текстовый вывод `ip route show` / `ip rule show` / `ip route get` + парсинг.
  Routing tables обнаруживаются через `ip rule show` (ищем `lookup <table>`) — работает с `--network host`
  и позволяет увидеть хостовые таблицы (напр. `100 vpn_kz`), которых нет в контейнерном `/etc/iproute2/rt_tables`.
- **RouteManager** хранит managed статические маршруты в `/etc/wireguard/data/routes.json`, восстанавливает при старте.
  Маршруты добавленные внешними скриптами (split routing) RouteManager не видит и не восстанавливает — это нормально.
- **HTTP method в fetch() — ВСЕГДА uppercase** (`method.toUpperCase()` в `api.js` `call()`).
  Node.js 22 llhttp отвергает lowercase методы (esp. `'patch'`) с 400 + TCP RST на уровне парсера,
  до попадания в h3 — симптом: `ERR_CONNECTION_RESET` + ничего в docker logs.
- **Toast-уведомления** вместо `alert()` — все 52 вызова заменены. Система: `toasts[]` в Vue data,
  `showToast(msg, type, duration)` / `dismissToast(id)`. HTML: `<transition-group>` + CSS slide animation.
  Зелёный = success, красный = error. Таймаут 7с (PSK-инструкция — 10с). Закрытие кнопкой ×.

---

## Общая концепция

При первом запуске контейнера автоматически создаётся один системный интерфейс — **Admin Instance**. Все остальные интерфейсы создаются пользователем вручную через UI. Admin Instance и пользовательские интерфейсы разделены — в UI они отображаются в разных секциях и не смешиваются.

### Структура UI (sidebar навигация)

- **Interfaces** — data plane интерфейсы: динамические вкладки, per-interface view (info card + peers list)
- **Gateways** — (coming soon)
- **Routing** — (coming soon)
- **Firewall / NAT** — (coming soon)
- **Settings** — глобальные настройки и AWG2 Templates
- **Administration** — Admin Tunnel (бывший Clients), системный интерфейс + его пиры

---

## 1. Admin Instance (управляющий план)

Системный AWG2 интерфейс. Администратор подключается к нему и затем открывает веб-UI. В перспективе доступ к веб-UI будет ограничен только подсетью этого интерфейса (на этапе разработки — открыт).

### Параметры задаются через env vars в run.sh / docker-compose.yml

| Переменная | Назначение | Пример |
|------------|-----------|--------|
| `WG_ADMIN_ADDRESS` | IP адрес интерфейса | `10.0.0.1/24` |
| `WG_HOST` | Публичный IP/домен сервера | `1.2.3.4` |
| `WG_PORT` | UDP порт | `51820` |
| `WG_DEFAULT_DNS` | DNS для клиентских конфигов | `1.1.1.1,8.8.8.8` |
| `JC` | AWG junk packet count | `6` |
| `JMIN` | AWG min junk size | `10` |
| `JMAX` | AWG max junk size | `50` |
| `S1..S4` | AWG size параметры | `64, 67, 17, 4` |
| `H1..H4` | AWG hash диапазоны | `221138202-537563446` |
| `I1..I5` | AWG imitation параметры | `<r 2> <b 0x...>` |
| `ITIME` | AWG timing | `0` |
| `PORT` | Порт веб-UI | `51821` |
| `PASSWORD_HASH` | bcrypt хэш пароля UI | `$2y$...` |

### Поведение при запуске
- Ключи генерируются автоматически при первом запуске, сохраняются в data
- При повторном запуске ключи берутся из файла — не перегенерируются
- Интерфейс поднимается автоматически

### Управление через UI
- Только добавление и удаление пиров
- Параметры самого интерфейса менять через UI нельзя — только через env vars + рестарт контейнера
- Нет кнопок Stop/Start/Edit для интерфейса

---

## 2. Data Plane Instances (пользовательские туннели)

Создаются пользователем вручную. Каждый Instance — отдельный WireGuard/AWG интерфейс.

### Назначение интерфейса

Назначение интерфейса определяется чекбоксом **Disable Routes** — отдельного поля "тип" нет:

| Disable Routes | Назначение | Поведение |
|----------------|-----------|-----------|
| ☐ Не отмечен (дефолт) | Подключение удалённых клиентов (телефоны, компьютеры, планшеты) | NAT включён, маршруты пиров добавляются в таблицу маршрутизации |
| ✅ Отмечен | P2P / P2MP линки между сетевыми устройствами | NAT отключён, маршруты не добавляются (`Table = off`) |

### Начальное состояние
При старте контейнера — нет ни одного data plane Instance.

### Форма создания / редактирования

| Параметр | Обяз. | Примечание |
|----------|-------|------------|
| Name | ✅ | Произвольное имя |
| IP адрес | ✅ | Адрес интерфейса, напр. `10.10.0.1/24` |
| Listen port | ✅ | UDP порт |
| Protocol | ✅ | `AmneziaWireGuard 2.0` или `WireGuard 1.0` |
| Private key | ✅ | Кнопка Generate (автозаполнение) или ручной ввод. После создания — **не редактируется, не отображается в открытом виде** |
| Public key | ✅ | Автозаполняется при Generate, или ручной ввод |
| Disable Routes | ☐ | Checkbox. Если отмечен → `Table = off` в конфиге **и** отключаются правила NAT (PostUp/PostDown без MASQUERADE). Для S2S/PBR сценариев. |
| AWG параметры | если AWG2 | Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5. Ручной ввод или импорт из JSON файла. |

### AWG параметры в форме
- Появляются только при выборе протокола AWG2
- Заполняются двумя способами:
  1. **Выбор профиля** — dropdown "Select obfuscation profile" из списка сохранённых профилей (Settings → AWG2 Profiles). При выборе все поля заполняются автоматически, включая H1-H4 диапазоны — копируются точно из профиля
  2. **Вручную** — пользователь вводит каждое поле самостоятельно
- Кнопка "Use Defaults" **удалена**
- После применения профиля параметры можно редактировать вручную
- При редактировании существующего интерфейса — тот же dropdown позволяет сменить профиль обфускации
- При выборе протокола WG1.0 — AWG параметры скрываются полностью

### Экспорт параметров обфускации AWG2
- Доступен для уже созданного интерфейса (не в форме создания)
- Кнопка "Export Obfuscation params" в карточке интерфейса (только для AWG2 интерфейсов)
- Скачивает JSON файл с текущими параметрами: Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5
- Формат файла совместим с импортом (используется тот же JSON-формат)

### Формат JSON файла параметров обфускации AWG2
```json
{
  "jc": 6,
  "jmin": 10,
  "jmax": 50,
  "s1": 64,
  "s2": 67,
  "s3": 17,
  "s4": 4,
  "h1": "221138202-271138202",
  "h2": "1295032551-1345032551",
  "h3": "2368926900-2418926900",
  "h4": "3442821249-3492821249",
  "i1": "",
  "i2": "",
  "i3": "",
  "i4": "",
  "i5": ""
}
```

### Инструмент запуска
- AWG2 → `awg-quick` (интерфейс типа `amneziawg`)
- Vanilla WG → `wg-quick` (интерфейс типа `wireguard`)

### Редактирование
- Доступно для любых параметров кроме приватного ключа
- После сохранения интерфейс **не перезапускается автоматически** — пользователь делает Stop→Start вручную

### Disable Routes
Checkbox в форме создания/редактирования. **По умолчанию не отмечен.**

- **Не отмечен (дефолт)** → `Table` не пишется в конфиг (wg-quick использует `auto`), NAT правила (MASQUERADE) создаются. Интерфейс предназначен для подключения удалённых клиентов.
- **Отмечен** → `Table = off` в конфиге **и** PostUp/PostDown не содержат MASQUERADE правил (только FORWARD ACCEPT). Интерфейс предназначен для P2P/P2MP линков между сетевыми устройствами с кастомным роутингом.

WireGuard по-прежнему использует AllowedIPs для крипторутинга в обоих случаях.

---

## 3. Peers

### Список пиров
- Фильтр по инстансу — dropdown в шапке вкладки (All / конкретный Instance)
- Для каждого пира отображается:
  - Имя
  - Instance (к которому привязан)
  - IP адрес
  - Индикатор активности (online/offline — по времени последнего handshake)
  - Трафик RX и TX (из `wg show` / `awg show`)
  - Toggle enable/disable (без удаления)
  - Кнопки: Edit, QR-код, Download config, Delete

---

### 3.1 Создание пира — Client интерфейс (Disable Routes = off)

Упрощённый быстрый флоу. Кнопка **"+New"** в шапке списка пиров.

**Диалог создания:** запрашивается только одно поле — **имя пира**.

**Кнопка:** `Create` (не "Create and show QR").

- После нажатия пир создаётся: ключи генерируются автоматически, IP назначается автоматически (следующий свободный в подсети интерфейса)
- QR-код и конфиг **не показываются автоматически** — доступны позднее через кнопки в карточке пира или через API
- Диалог закрывается, новый пир появляется в списке

---

### 3.2 Создание пира — Interconnect интерфейс (Disable Routes = on)

Два режима создания, выбираются кнопками в шапке списка пиров:

#### Режим 1: Manual (кнопка "Manual")
Открывается форма ручного ввода:

| Параметр | Обяз. | Примечание |
|----------|-------|------------|
| Name | ✅ | Произвольное имя |
| Address | ✅ | Туннельный IP пира в CIDR. Ручной ввод с валидацией |
| Public key | ✅ | Публичный ключ удалённой стороны. Ручной ввод |
| Pre-shared key | ☐ | Кнопка **"Generate PSK"** или ручной ввод. Кнопка **"Copy PSK"**. Координируется с удалённой стороной вручную — обе стороны должны использовать одинаковый PSK |
| Endpoint | ☐ | `IP:port` удалённой стороны. Опциональный |
| PersistentKeepalive | ☐ | Секунды. Опциональный |
| AllowedIPs | ✅ | Туннельный IP удалённой стороны `/32` — используется в локальном WireGuard `[Peer]` для крипторутинга |
| Client AllowedIPs | ✅ | Что удалённая сторона будет маршрутизировать через нас — идёт в экспортируемый файл. По умолчанию `0.0.0.0/0` |

**Примечание:** для Interconnect пира приватный ключ **не хранится** на сервере. QR и Download config недоступны.

#### Режим 2: Import from JSON (кнопка "Import JSON")
- Открывается диалог выбора JSON файла
- После выбора файла все поля заполняются автоматически из файла
- Пользователь может отредактировать поля перед сохранением
- Формат файла — см. раздел 3.3

---

### 3.3 Два поля AllowedIPs у Interconnect пира

| Поле | Значение | Где используется |
|------|----------|-----------------|
| `allowedIPs` | Туннельный IP удалённой стороны `/32`, напр. `10.20.0.2/32` | Локальный WireGuard `[Peer]` конфиг — крипторутинг |
| `clientAllowedIPs` | `0.0.0.0/0` (по умолчанию) | Экспортируемый файл для удалённой стороны — что remote пустит через туннель к нам |

---

### 3.4 Формат JSON файла конфигурации Interconnect пира

Используется как для **импорта** (создание пира из файла), так и для **экспорта** (передача параметров удалённой стороне).

```json
{
  "name": "Site-A",
  "publicKey": "...",
  "presharedKey": "...",
  "endpoint": "1.2.3.4:51830",
  "persistentKeepalive": 25,
  "allowedIPs": "10.20.0.1/32",
  "clientAllowedIPs": "0.0.0.0/0"
}
```

**Поля:**
- `allowedIPs` — туннельный IP экспортирующей стороны `/32` (будет использован как AllowedIPs в `[Peer]` при импорте)
- `clientAllowedIPs` — что импортирующая сторона будет маршрутизировать через этот пир
- `presharedKey` — должен совпадать на обеих сторонах, координируется вручную

---

### 3.5 Валидация адреса пира
- Должен принадлежать подсети выбранного Instance
- Не может совпадать с адресом интерфейса Instance
- Не может совпадать с адресом уже существующего пира
- Для Client пиров маска всегда `/32` — не задаётся пользователем
- Для Interconnect пиров маска задаётся вручную (могут быть подсети `/24`, `/30` и т.д.)

---

### 3.6 Данные активности (polling из wg show)
- **Online** — последний handshake < 3 минут назад
- **Offline** — последний handshake > 3 минут назад или отсутствует
- RX / TX в читаемом виде (KB, MB, GB)

---

### 3.7 QR-код и Download config (только для Client пиров)
Доступны т.к. приватный ключ хранится на сервере. Генерируют полный клиентский конфиг:
```
[Interface]
PrivateKey = ...
Address = <peer IP>/<iface mask>
DNS = <из глобальных настроек>

[Peer]
PublicKey = <публичный ключ инстанса>
PresharedKey = ...
Endpoint = <WG_HOST>:<port инстанса>
AllowedIPs = <Client AllowedIPs>
PersistentKeepalive = ...
```
AWG2 параметры добавляются в `[Interface]` если инстанс использует AWG2.

Для Interconnect пиров QR и Download config **недоступны** (приватный ключ не хранится на сервере).

---

## 4. Settings

### 4.1 Global Settings

Применяются по умолчанию ко всем новым пирам.

| Параметр | Дефолт | Примечание |
|----------|--------|------------|
| DNS | `1.1.1.1, 8.8.8.8` | DNS серверы в клиентских конфигах |
| Default PersistentKeepalive | `25` | Секунды |
| Default Client AllowedIPs | `0.0.0.0/0, ::/0` | Full tunnel по умолчанию |

### 4.2 AWG2 Обфускация — Профили (Templates)

Именованные наборы AWG2 параметров обфускации. Хранятся в Settings, используются при создании и редактировании AWG2 интерфейсов.

**Структура профиля:**

| Поле | Тип | Примечание |
|------|-----|------------|
| Name | string | Имя профиля |
| Default | boolean | Один профиль помечен как дефолтный |
| Jc | number | |
| Jmin | number | |
| Jmax | number | |
| S1, S2, S3, S4 | number | |
| H1, H2, H3, H4 | string | Диапазоны (напр. `221138202-271138202`). Копируются как есть — **обе стороны туннеля должны иметь одинаковые диапазоны** |
| I1..I5 | string | Свободный текст. Поддержка `<r 2> <b 0x...>` |

**Управление профилями в Settings:**
- Создание, редактирование, удаление профилей
- Назначение профиля дефолтным
- **Импорт профиля из JSON** — кнопка "Import JSON" → выбор файла → создаётся новый профиль с параметрами из файла
- **Экспорт профиля в JSON** — кнопка "Export JSON" на каждом профиле → скачивает JSON файл с параметрами профиля
- Формат JSON файла — тот же что и для параметров обфускации интерфейса (см. раздел 2)

**Использование профиля при создании/редактировании AWG2 интерфейса:**
- Dropdown "Select obfuscation profile" — список всех сохранённых профилей
- При выборе профиля все поля AWG2 параметров копируются из профиля как есть, включая H1-H4 диапазоны
- После применения профиля параметры можно редактировать вручную
- При редактировании существующего интерфейса — тот же dropdown позволяет сменить профиль
- Параметры **копируются** в интерфейс (не ссылка) — изменение профиля в Settings не влияет на уже настроенные интерфейсы

**AWG2 полная обфускация (I1):**
По данным сообщества (письменной документации нет): полная обфускация включается когда `I1` начинается с `<r 2>`, за которым следует `<b 0x...>` (бинарный blob, имитирующий DNS или другой протокол).

Пример:
```
<r 2> <b 0x084481800001000300000000077469636b65747306776964676574096b696e6f706f69736b0272750000010001...>
```

---

## 5. Технические детали (важно для реализации)

### iptables и NAT
- Ubuntu 22.04 использует nftables как дефолтный бэкенд
- Docker устанавливает FORWARD policy DROP через nftables
- PostUp **обязан** использовать `iptables-nft` (не `iptables`)

**Если `disableRoutes = false` (Client интерфейс, дефолт):**
PostUp добавляет FORWARD ACCEPT + MASQUERADE:
```
PostUp = iptables-nft -I FORWARD -i <iface> -j ACCEPT; iptables-nft -I FORWARD -o <iface> -j ACCEPT; iptables-nft -t nat -A POSTROUTING -s <subnet> -j MASQUERADE
PostDown = iptables-nft -D FORWARD -i <iface> -j ACCEPT; iptables-nft -D FORWARD -o <iface> -j ACCEPT; iptables-nft -t nat -D POSTROUTING -s <subnet> -j MASQUERADE
```

**Если `disableRoutes = true` (Site-to-Site интерфейс):**
PostUp добавляет только FORWARD ACCEPT, без MASQUERADE:
```
PostUp = iptables-nft -I FORWARD -i <iface> -j ACCEPT; iptables-nft -I FORWARD -o <iface> -j ACCEPT
PostDown = iptables-nft -D FORWARD -i <iface> -j ACCEPT; iptables-nft -D FORWARD -o <iface> -j ACCEPT
```
Трансляция адресов не выполняется — трафик маршрутизируется "как есть".

### --network host и "already exists"
С `--network host` WireGuard интерфейсы живут в ядре хоста и переживают `docker stop/start`. При повторном `awg-quick up` падает с "already exists" → PostUp не выполняется → нет NAT. Решение: при старте интерфейса всегда регенерировать конфиг, при "already exists" делать down→up цикл.

### Адрес пира в клиентском конфиге
`[Interface] Address` в клиентском конфиге вычисляется из IP пира (из AllowedIPs) + маски интерфейса Instance. Не хранится отдельно, не запрашивается у пользователя.

### Приватный ключ
- Хранится на сервере в data (нужен для генерации клиентских конфигов и QR-кодов)
- В UI всегда отображается как password-поле (символы скрыты)
- Не редактируется после создания
- Зарезервировано на будущее: опциональный показ после OTP-аутентификации

### WireGuard и broadcast
WireGuard использует `tun` интерфейс (L3), broadcast не поддерживается. Маска `/32` для пиров — правильный выбор, подсеть в AllowedIPs используется только для крипторутинга, а не для L2 broadcast.

---

## 6. Frontend — ограничения и паттерны (обязательно к прочтению)

### ⚠️ Tailwind CSS — статическая сборка (КРИТИЧНО)

`src/www/css/app.css` — **прекомпилированный** Tailwind v3.4.10. В нём только те классы, которые были использованы на момент сборки. **Новые Tailwind-классы не появятся сами по себе** — нет postcss/JIT в runtime, нет watch-режима.

**Правило:** если нужен новый CSS-класс которого нет в `app.css` — **использовать inline `style="..."`**.

Проверить наличие класса:
```bash
grep "px-6\|py-10" src/www/css/app.css
```

Зафиксированные прецеденты:
- `px-6`, `py-10`, `py-8` — **отсутствуют** в app.css → использовать `style="padding: ..."`
- `px-4`, `sm:px-6` — присутствуют

---

### Паттерн модальных окон (единый для всех модалок)

Все модалки должны использовать один паттерн — **scroll на overlay, не на panel**. Это обеспечивает правильный скролл вверх/вниз и видимые отступы от краёв.

```html
<!-- ✅ ПРАВИЛЬНО -->
<div v-if="showModal"
  class="fixed inset-0 bg-black bg-opacity-50 z-50 overflow-y-auto"
  @click.self="showModal = false">

  <div class="flex min-h-full items-start justify-center" style="padding: 40px 24px;">
  <div class="bg-white dark:bg-neutral-700 rounded-lg w-full" style="max-width: 520px;">

    <!-- заголовок -->
    <div class="p-6 pb-4 border-b dark:border-neutral-600">
      <h2>...</h2>
    </div>

    <!-- контент (без overflow-y-auto, без flex-1) -->
    <div class="p-6 pt-4">
      ...
    </div>

    <!-- футер -->
    <div class="p-6 pt-4 border-t dark:border-neutral-600">
      <div class="flex gap-3 justify-end">...</div>
    </div>

  </div>
  </div><!-- /flex wrapper -->
</div>
```

```html
<!-- ❌ НЕПРАВИЛЬНО — items-center + overflow-y-auto на одном div -->
<div class="fixed inset-0 flex items-center justify-center overflow-y-auto ...">
  <div class="max-h-[90vh] flex flex-col ...">
    <div class="flex-1 overflow-y-auto ...">  <!-- внутренний скролл -->
```

**Почему неправильно:** `flex items-center` вычисляет центр по всему контенту, а не по viewport. При переполнении верх модала уходит за экран. Вниз скролл работает, вверх — нет.

**Что запрещено в panel:** `max-h-[90vh]`, `flex flex-col` с ограниченной высотой, `flex-1 overflow-y-auto` внутри, `flex-shrink-0` на header/footer.

---

### Vue 2 — реактивность массивов

Vue 2 не отслеживает изменения через индекс (`array[i] = x`) и через полную замену (`this.list = newList` — ненадёжно).

```javascript
// ✅ ПРАВИЛЬНО — splice для обновления одного элемента:
this.tunnelInterfaces.splice(idx, 1, updatedItem);

// ✅ ПРАВИЛЬНО — push для добавления:
this.tunnelInterfaces.push(newItem);

// ❌ НЕПРАВИЛЬНО:
this.tunnelInterfaces[idx] = updatedItem;  // Vue 2 не увидит изменение
```

---

### Структура файлов UI

| Файл | Роль |
|------|------|
| `src/www/index.html` | Весь шаблон Vue (один файл, ~1400 строк). Sidebar + `<main>` content area. |
| `src/www/js/app.js` | Vue instance: `data`, `methods`, `computed`, `watch`, `mounted` |
| `src/www/js/api.js` | Все HTTP запросы к backend API |
| `src/www/css/app.css` | Скомпилированный Tailwind (не редактировать вручную, многие классы отсутствуют) |

При добавлении новой страницы: добавить элемент в `sidebarMenu[]`, добавить `v-if="activePage === '...'"` секцию в `<main>`, добавить данные и методы в `app.js`, добавить API методы в `api.js`.

---

## Анализ миграции на Go+Fiber

> Дата анализа: 2026-03-12

### Что переписывается / что остаётся

**Фронтенд — не трогается** (6 354 строки): `app.js`, `api.js`, `index.html`, `i18n.js`, `app.css` остаются as-is. Единственное требование — сохранить идентичный API-контракт (JSON-ответы).

**Бэкенд — полный переписыш** (~6 500 строк Node.js → ~9 000–11 000 строк Go, Go многословнее):

| Файл | Строк | Сложность |
|------|-------|-----------|
| `Server.js` | 1 555 | Средняя — 50+ роутов, сессии |
| `TunnelInterface.js` | 860 | **Высокая** — mutex, AWG-специфика |
| `WireGuard.js` | 564 | Средняя — легаси, но рабочий |
| `InterfaceManager.js` | 378 | Низкая |
| `RouteManager.js` | 354 | Средняя — текстовый парсинг `ip` |
| `Peer.js` | 330 | Низкая |
| `Settings.js` | 284 | Низкая |
| TunnelManager + WanTunnel + Gateways | ~1 000 | Средняя |
| `Util.js` | 91 | Низкая |

### Прямые соответствия (простые замены)

| Node.js | Go | Примечание |
|---------|-----|-----------|
| `h3` | `gofiber/fiber` | почти 1:1 синтаксис роутинга |
| `bcryptjs` | `golang.org/x/crypto/bcrypt` | идентично |
| `uuid` | `google/uuid` | идентично |
| `qrcode` | `skip/go-qrcode` | идентично |
| `fs.promises.readFile/writeFile` | `os.ReadFile/WriteFile` | проще |
| `childProcess.exec` + timeout + SIGKILL | `exec.CommandContext` + `context.WithTimeout` | **чище в Go** |
| `express-session` | `gofiber/contrib/session` | plug-and-play |
| `debug` | `log/slog` или `zerolog` | конфигурируется явнее |

### Ключевой выигрыш: Promise-chain mutex → sync.Mutex

```javascript
// JS (FIX-8/9): хак через Promise chain
this._reloadMutex = this._reloadMutex
  .then(async () => { await this.restart(); })
  .catch(() => {});
```

```go
// Go: нативный sync.Mutex — надёжнее и проще
type TunnelInterface struct { mu sync.Mutex }
func (t *TunnelInterface) kernelSetPeer(peer *Peer) error {
    t.mu.Lock(); defer t.mu.Unlock()
    return t.reload()
}
```

Go-mutex — это улучшение, не проблема. Устраняет весь класс deadlock-рисков.

### Что усложняется в Go

1. **Типизация везде** — structs + json tags на каждую модель, ~1.5–2x объём кода
2. **Error handling** — нет try/catch, везде `if err != nil`
3. **Session middleware** — менее "магическое", чем express-session, но прозрачнее

### Оценка трудоёмкости

| Компонент | Дней |
|-----------|------|
| Fiber router, сессии, static serving | 2–3 |
| Settings + Templates | 1–2 |
| Peer model | 1–2 |
| InterfaceManager | 1–2 |
| RouteManager | 2 |
| TunnelInterface (самый сложный) | 5–7 |
| WireGuard.js (легаси) | 2–3 |
| Gateway/Monitor | 2–3 |
| Dockerfile (Go multi-stage build) | 0.5 |
| Integration testing (preserve all FIX-*) | 3–5 |
| **Итого** | **~20–30 рабочих дней** |

### Выигрыш от миграции

| Параметр | Node.js | Go |
|----------|---------|-----|
| Docker image | ~200 MB (node_modules) | ~15–20 MB (статический бинарь) |
| RAM idle | ~80–150 MB | ~15–30 MB |
| Startup | ~2–3 сек | ~100–200 мс |
| Mutex | Promise chain hack | Нативный sync.Mutex |
| Type safety | Runtime errors | Compile-time |

### Главный риск

**Сохранение всех 12 критических фиксов** (`_reloadMutex`, `ip -j` запрет, `awg syncconf` вместо `awg set peer`, iptables-nft, etc.). Каждый из них должен быть осознанно воспроизведён — механически перевести их нельзя.

### Вывод

**Технически достижимо, экономически спорно.** Фронтенд (50% кодовой базы) не меняется. Выигрыш — меньший Docker-образ и нативные примитивы конкурентности. При наличии опытного Go-разработчика — 4–6 недель с тестированием на реальном железе.
