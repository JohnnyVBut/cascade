# Cascade — Архитектурный документ

> Версия: 2026-04-01. При добавлении/изменении/удалении любой фичи — обновить этот документ **в том же коммите**.

---

## 1. Обзор проекта

**Cascade** (бывший AWG-Easy 3.0) — это веб-приложение для управления WireGuard и AmneziaWG маршрутизатором. Предоставляет полноценный UI и REST API для:

- управления WireGuard/AWG туннельными интерфейсами и пирами;
- настройки Firewall (iptables-nft с кастомными цепочками);
- Policy-Based Routing (PBR, fwmark + ip rule + ip route);
- управления маршрутами (статические + просмотр ядра);
- Outbound NAT (MASQUERADE/SNAT);
- мониторинга шлюзов (ICMP ping + HTTP probe, Gateway Groups, failover);
- Firewall Aliases (host/network/ipset/group/port/port-group);
- генерации AWG2 параметров (7 CPS-профилей);
- многопользовательской аутентификации с TOTP 2FA и API-токенами.

### Технологический стек

| Слой | Технология |
|------|-----------|
| Backend | Go 1.23, Fiber v2 (HTTP framework) |
| База данных | SQLite via `modernc.org/sqlite` (CGO-free, чисто Go) |
| Frontend | Vue 2 (CDN, без сборки), Tailwind CSS (CDN), VueI18n, ApexCharts |
| WireGuard | `awg-quick` / `awg` (AmneziaWG 2.0), `wg-quick` / `wg` (WireGuard 1.0) |
| QR коды | `rsc.io/qr` (pure Go SVG) |
| TOTP | `github.com/pquerna/otp` |
| Деплой | Docker, `--network host`, Caddy reverse proxy |

### Репозиторий

- Repo: `git@github.com:JohnnyVBut/cascade.git`
- Основная ветка: `master`
- Точка входа: `/Users/jenya/PycharmProjects/cascade/cmd/awg-easy/main.go`

---

## 2. Структура проекта

```
cascade/
├── cmd/
│   └── awg-easy/
│       └── main.go              ← точка входа: конфигурация, инициализация, HTTP сервер
├── internal/
│   ├── aliases/                 ← Firewall Aliases (host/network/ipset/group/port/port-group)
│   │   ├── manager.go           ← CRUD, GetMatchSpec, GetPortMatchSpec
│   │   ├── manager.go           ← (основной файл)
│   │   └── aliases_test.go
│   ├── api/                     ← HTTP handlers (Fiber), каждый файл = один ресурс
│   │   ├── auth.go              ← сессии, логин/логаут, TOTP verify, AuthMiddleware
│   │   ├── compat.go            ← legacy shims (/api/lang, /api/release, /wireguard/client)
│   │   ├── interfaces.go        ← /api/tunnel-interfaces CRUD + lifecycle
│   │   ├── peers.go             ← /api/tunnel-interfaces/:id/peers CRUD
│   │   ├── routing.go           ← /api/routing/*
│   │   ├── firewall.go          ← /api/firewall/*
│   │   ├── gateways.go          ← /api/gateways/* + /api/gateway-groups/*
│   │   ├── nat.go               ← /api/nat/*
│   │   ├── aliases.go           ← /api/aliases/*
│   │   ├── settings.go          ← /api/settings + /api/templates
│   │   ├── users.go             ← /api/users/* + TOTP setup
│   │   ├── tokens.go            ← /api/tokens/*
│   │   └── users_admin_test.go
│   ├── awgparams/               ← Генератор AWG2 параметров (9 CPS-профилей)
│   │   ├── generator.go
│   │   └── generator_test.go
│   ├── db/                      ← SQLite lifecycle + migrations (v1..v10)
│   │   ├── db.go
│   │   └── db_test.go
│   ├── firewall/                ← FirewallManager: iptables цепочки, PBR, fallback
│   │   ├── manager.go           ← 1414 строк
│   │   └── firewall_test.go
│   ├── frontend/                ← embed.FS (Vue 2 SPA вшит в бинарник)
│   │   ├── embed.go
│   │   └── www/                 ← фронтенд файлы (index.html, js/app.js, css/app.css, ...)
│   ├── gateway/                 ← GatewayManager + Monitor (ICMP + HTTP probe)
│   │   ├── manager.go
│   │   ├── monitor.go
│   │   └── types.go
│   ├── ipset/                   ← IpsetManager: kernel ipsets для alias matching
│   │   ├── manager.go
│   │   └── prefixes.go          ← PrefixFetcher (RIPE RISwhois BGP данные)
│   ├── nat/                     ← NatManager: MASQUERADE/SNAT iptables правила
│   │   ├── manager.go
│   │   └── nat_test.go
│   ├── peer/                    ← Peer model: CRUD, config generation, QR codes
│   │   ├── peer.go
│   │   └── peer_test.go
│   ├── routing/                 ← RouteManager: статические маршруты + kernel views
│   │   ├── manager.go
│   │   └── manager_test.go
│   ├── settings/                ← GlobalSettings + AWG2 Templates (SQLite key/value)
│   │   ├── settings.go
│   │   └── settings_test.go
│   ├── tokens/                  ← API tokens (SHA-256 hash, prefix "ws_")
│   │   ├── tokens.go
│   │   └── tokens_test.go
│   ├── tunnel/                  ← TunnelInterface + Manager (WG/AWG lifecycle)
│   │   ├── interface.go         ← 1110 строк
│   │   ├── manager.go
│   │   └── tunnel_test.go
│   ├── users/                   ← Multi-user auth (bcrypt cost 12, TOTP)
│   │   ├── users.go
│   │   └── users_test.go
│   ├── util/                    ← exec helpers (timeout, logging), net helpers
│   │   ├── exec.go
│   │   ├── net.go
│   │   └── util_test.go
│   └── validate/                ← Input validation (инъекция команд prevention)
│       ├── validate.go
│       └── validate_test.go
├── deploy/
│   ├── caddy/                   ← Caddy reverse proxy (TLS, hidden admin path, decoy)
│   │   ├── Caddyfile
│   │   ├── Dockerfile
│   │   ├── docker-compose.yml
│   │   ├── scripts/
│   │   │   └── acme-install.sh  ← Let's Encrypt cert для bare IP
│   │   └── www/                 ← Decoy site файлы
│   ├── setup.sh                 ← первоначальный деплой
│   └── switch-mode.sh           ← переключение режимов (host/bridge/isolated)
├── cmd/
│   └── awg-easy/main.go
├── Dockerfile.go                ← multi-stage: builder (Go 1.23) + runtime (amneziawg-go)
├── docker-compose.go.yml        ← production docker-compose
├── go.mod                       ← module github.com/JohnnyVBut/cascade, Go 1.23
└── go.sum
```

---

## 3. Архитектура Backend

### 3.1 Пакет `db`

**Назначение:** открыть/создать SQLite базу, прогнать pending migrations, предоставить глобальный `*sql.DB`.

**Ключевые решения:**
- `modernc.org/sqlite` — pure Go (CGO_ENABLED=0), статический бинарник
- WAL journal mode: concurrent reads + serialized writes
- `MaxOpenConns=1`: предотвращает "database is locked"
- `PRAGMA busy_timeout=5000`: ожидание до 5s вместо немедленного SQLITE_BUSY
- `PRAGMA foreign_keys=ON`: каскадное удаление пиров при удалении интерфейса

**Публичный API:**
```go
func Init(dataDir string) error       // открыть/создать DB, прогнать migrations
func DB() *sql.DB                     // получить глобальный handle (panic если не вызван Init)
func Close()                          // закрыть при graceful shutdown
```

**Хранение:** `<dataDir>/wireguard.db`

**Migrations:** v1..v10, версия хранится в таблице `schema_migrations`. Никогда не изменять существующие миграции — только добавлять новые.

---

### 3.2 Пакет `tunnel`

**Назначение:** управление WireGuard/AmneziaWG интерфейсами (`wg10`, `wg11`, ...) — lifecycle (start/stop/reload), peers, конфиг генерация, status polling.

**Ключевые типы:**

```go
type TunnelInterface struct {
    reloadMu  sync.Mutex   // сериализует Reload/Restart — concurrent syncconf = deadlock
    peersMu   sync.RWMutex // защищает in-memory peers map
    // Persisted:
    ID, Name, Address, Protocol string
    ListenPort    int
    Enabled       bool
    DisableRoutes bool        // true = Table=off (S2S/PBR сценарии)
    PrivateKey    string      // никогда не отдаётся в JSON
    PublicKey     string
    AWG2          *peer.AWG2Settings  // nil для wireguard-1.0
    CreatedAt     string
    // Runtime (not persisted):
    peers map[string]*peer.Peer
}

type Manager struct {
    mu         sync.RWMutex
    interfaces map[string]*TunnelInterface
    stopCh     chan struct{}
    WGHost     string
}
```

**Публичные методы Manager:**

| Метод | Описание |
|-------|----------|
| `Init(wgHost string) (*Manager, error)` | Singleton — загружает интерфейсы из SQLite, auto-start enabled |
| `Get() *Manager` | Получить singleton (nil до Init) |
| `CreateInterface(inp CreateInput) (*TunnelInterface, error)` | Генерирует ключи, назначает ID/порт, создаёт в SQLite |
| `GetInterface(id string) *TunnelInterface` | Получить по ID |
| `GetAllInterfaces() []*TunnelInterface` | Все интерфейсы, sorted by CreatedAt ASC |
| `UpdateInterface(id string, upd InterfaceUpdate) (*TunnelInterface, error)` | PATCH → save → syncconf |
| `DeleteInterface(id string) error` | Stop → удалить пиров → удалить из SQLite + диска |
| `StartInterface(id string) (*TunnelInterface, error)` | `awg-quick/wg-quick up` |
| `StopInterface(id string) (*TunnelInterface, error)` | `awg-quick/wg-quick down` |
| `RestartInterface(id string) (*TunnelInterface, error)` | Stop + Start |
| `AddPeer(ifaceID string, inp peer.PeerInput) (*peer.Peer, error)` | Создать пир + kernel sync |
| `UpdatePeer(ifaceID, peerID string, upd peer.PeerUpdate) (*peer.Peer, error)` | PATCH пира |
| `RemovePeer(ifaceID, peerID string) error` | Удалить + kernel sync |
| `GetAllPeers() []*peer.Peer` | Все пиры всех интерфейсов (dashboard) |
| `GetPeerRemoteConfig(ifaceID, peerID string) (string, error)` | Генерация клиентского WG конфига |

**Автоматические ID/порты:**
- Интерфейсы нумеруются начиная с `wg10` (wg10, wg11, ...)
- Порты начиная с `51830`

**Протокол-специфичные бинарники (FIX-7):**
```go
func (t *TunnelInterface) quickBin() string {
    if t.Protocol == "amneziawg-2.0" { return "awg-quick" }
    return "wg-quick"
}
func (t *TunnelInterface) syncBin() string {
    if t.Protocol == "amneziawg-2.0" { return "awg" }
    return "wg"
}
```

**Зависимости:** `db`, `peer`, `util`, `validate`, `settings`

---

### 3.3 Пакет `peer`

**Назначение:** CRUD пиров в SQLite, генерация клиентского WG конфига, QR-код (SVG), генерация ключей.

**Ключевые типы:**

```go
type Peer struct {
    // Persisted:
    ID, InterfaceID, Name         string
    PublicKey, PrivateKey         string
    PresharedKey                  string
    Endpoint, AllowedIPs, Address string
    ClientAllowedIPs              string
    PeerType                      string  // "client" | "interconnect"
    PersistentKeepalive           int
    Enabled                       bool
    CreatedAt, UpdatedAt          string
    ExpiredAt, OneTimeLink        string
    DownloadableConfig            bool    // computed: PrivateKey != ""
    // Runtime (NOT persisted, из wg/awg show dump):
    TransferRx, TransferTx        int64
    LatestHandshakeAt             *string
    RuntimeEndpoint               string
}

type AWG2Settings struct {
    Jc, Jmin, Jmax             int
    S1, S2, S3, S4             int
    H1, H2, H3, H4             string  // "start-end" range (FIX-4)
    I1, I2, I3, I4, I5         string
}
```

**Публичные функции:**

| Функция | Описание |
|---------|----------|
| `GetPeers(interfaceID string) ([]Peer, error)` | Все пиры интерфейса из SQLite |
| `GetPeer(id string) (*Peer, error)` | Один пир по ID |
| `CreatePeer(interfaceID string, inp PeerInput) (*Peer, error)` | Вставить в SQLite |
| `UpdatePeer(id string, upd PeerUpdate) (*Peer, error)` | Обновить поля в SQLite |
| `DeletePeer(id string) error` | Удалить из SQLite |
| `GenerateKeys(syncBin string) (KeyPair, error)` | `wg genkey` + `wg pubkey` + `wg genpsk` |
| `GeneratePSK(syncBin string) (string, error)` | `wg genpsk` |
| `(p *Peer) ToWgConfig() string` | `[Peer]` секция для wg-quick конфига |
| `(p *Peer) GenerateRemoteConfig(iface InterfaceData) string` | Полный клиентский конфиг |
| `(p *Peer) QRCodeSVG(iface InterfaceData) (string, error)` | SVG QR-код |

**Зависимости:** `db`, `util`

---

### 3.4 Пакет `settings`

**Назначение:** глобальные настройки приложения + AWG2 шаблоны. Хранение: SQLite таблицы `settings` (key/value) и `templates`.

**Ключевые типы:**

```go
type GlobalSettings struct {
    DNS                        string
    DefaultPersistentKeepalive int
    DefaultClientAllowedIPs    string
    GatewayWindowSeconds       int
    GatewayHealthyThreshold    float64
    GatewayDegradedThreshold   float64
    RouterName                 string
    PublicIPMode               string   // "auto" | "manual"
    PublicIPManual             string
}

type Template struct {
    ID, Name  string
    IsDefault bool
    // AWG2 params (Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5)
}
```

**Публичные функции:**

| Функция | Описание |
|---------|----------|
| `GetSettings() (*GlobalSettings, error)` | Читает из SQLite, fallback на defaults |
| `UpdateSettings(updates map[string]any) (*GlobalSettings, error)` | Partial update (upsert) |
| `GetWGHost(override string) string` | Резолв публичного IP: ENV → manual → auto-detect |
| `GetTemplates() ([]Template, error)` | Все шаблоны |
| `GetTemplate(id string) (*Template, error)` | Один шаблон |
| `CreateTemplate(t Template) (*Template, error)` | Создать шаблон |
| `UpdateTemplate(id string, upd TemplateUpdate) (*Template, error)` | Обновить |
| `DeleteTemplate(id string) error` | Удалить |
| `SetDefaultTemplate(id string) error` | Установить дефолтный (сбрасывает остальные) |
| `ApplyTemplate(id string) (*AWG2Params, error)` | Fresh H1-H4 ranges + params |
| `ResolvePublicIP(mode, manual string) (string, error)` | Auto-detect через внешние сервисы |

**Зависимости:** `db`

---

### 3.5 Пакет `firewall`

**Назначение:** управление iptables-nft правилами — пакетная фильтрация (ACCEPT/DROP/REJECT) и Policy-Based Routing (PBR) через кастомные цепочки.

**Архитектура цепочек:**
- `FIREWALL_FORWARD` (filter table) — ACCEPT/DROP/REJECT для каждого правила
- `FIREWALL_MANGLE` (mangle table) — MARK (PBR правила) или RETURN (не-PBR)

**Ключевые типы:**

```go
type Rule struct {
    ID, Name          string
    Enabled           bool
    Order             int
    Interface         string    // any | wg10 | eth0 ...
    Protocol          string    // any | tcp | udp | tcp/udp | icmp
    Source, Destination Endpoint
    Action            string    // accept | drop | reject
    GatewayID         string    // PBR: direct gateway
    GatewayGroupID    string    // PBR: gateway group
    Fwmark            *int      // auto-assigned для PBR правил
    FallbackToDefault bool      // fallback to system default gw vs blackhole
}

type Endpoint struct {
    Type        string  // any | cidr | alias
    Value       string  // CIDR для type=cidr
    AliasID     string  // alias ID для type=alias
    Invert      bool    // !match
    PortAliasID string  // port/port-group alias ID
}

type Manager struct {
    am             *aliases.Manager
    gm             *gateway.Manager
    rebuildMu      sync.Mutex
    fallbackActive map[string]bool        // rule ID → currently in fallback
    restoreTimers  map[string]*time.Timer // rule ID → 30s anti-flap timer
}
```

**Публичные методы:**

| Метод | Описание |
|-------|----------|
| `New(am, gm) *Manager` | Создать Manager |
| `Init() error` | Инициализировать цепочки, загрузить правила, зарегистрировать gateway callback |
| `RebuildChains() error` | Публичный wrapper: flush + re-apply all enabled rules |
| `GetRules() ([]Rule, error)` | Все правила, sorted by order |
| `AddRule(inp RuleInput) (*Rule, error)` | Создать + rebuild chains |
| `UpdateRule(id string, inp RuleInput) (*Rule, error)` | Обновить + rebuild |
| `ToggleRule(id string, enabled bool) (*Rule, error)` | Включить/выключить |
| `DeleteRule(id string) error` | Удалить + rebuild |
| `MoveRule(id string, direction string) error` | up/down order + rebuild |
| `SimulateTrace(srcIP, dstIP string) (*TraceResult, error)` | PBR трассировка пакета |
| `GetNetworkInterfaces() ([]HostInterface, error)` | Хостовые интерфейсы |

**SimulateTrace алгоритм (FIX-GO-8, FIX-GO-10):**
1. Получить все включённые правила, sorted by order
2. Для каждого правила проверить совпадение: src CIDR или alias match, dst CIDR или alias match
3. При `type=alias`: host/network → `ipInCIDR()` (через `net.ParseCIDR` + `Contains`), ipset → `ipset test <name> <ip>`
4. Первое совпадающее правило с fwmark → вернуть как MatchedRule
5. Используется в `testRoute` handler для `ip route get <dst> mark <fwmark>`

**КРИТИЧНО (FIX-GO-10):** ipInCIDR реализован через `net.ParseCIDR(cidr)` + `ipNet.Contains(ip)`. Ручная битовая маска (`^uint32(0) >> prefixLen`) давала неверный результат для не-network-address IP.

**Зависимости:** `aliases`, `gateway`, `db`, `util`, `validate`

---

### 3.6 Пакет `routing`

**Назначение:** управление статическими маршрутами (SQLite) и read-only представление ядровой таблицы маршрутизации.

**Ключевые типы:**

```go
type Route struct {
    ID, Description, Destination string
    Gateway, Dev                  string
    Metric                        *int    // nil = no explicit metric
    Table                         string  // "main" | "100" | "vpn_kz" ...
    Enabled                       bool
}

type KernelRoute struct { Dst, Gateway, Dev, Protocol, Scope, PrefSrc, Table string; Metric int }
type RoutingTable struct { ID *int; Name string }   // ID=nil для синтетической "all" записи
type Manager struct{}  // stateless, все данные в SQLite
```

**Публичные методы:**

| Метод | Описание |
|-------|----------|
| `New() *Manager` | Создать (stateless) |
| `RestoreAll()` | Apply all enabled routes to kernel (FIX-13: после tunnel.Init) |
| `ReapplyForDevice(devName string)` | Re-add routes for specific interface (после start/restart) |
| `GetRoutes() ([]Route, error)` | Статические маршруты из SQLite |
| `AddRoute(inp RouteInput) (*Route, error)` | Создать + `ip route add` |
| `UpdateRoute(id string, inp RouteInput) (*Route, error)` | Обновить |
| `ToggleRoute(id string, enabled bool) (*Route, error)` | Включить/выключить + ip route |
| `DeleteRoute(id string) error` | Удалить + `ip route del` |
| `GetKernelRoutes(table string) ([]KernelRoute, error)` | `ip route show table <t>` (текстовый парсинг) |
| `GetRoutingTables() ([]RoutingTable, error)` | rt_tables + `ip rule show` (текстовый парсинг) |
| `TestRoute(dst string, mark *int) (*RouteResult, error)` | `ip route get <dst> [mark N]` |

**КРИТИЧНО (FIX-11):** команда `ip -j` ЗАПРЕЩЕНА во всём проекте. Флаг `-j` зависает навсегда на некоторых ядрах Linux (netlink API path). Использовать только текстовый вывод + парсинг.

**FIX-15:** ошибки `ip route` пробрасываются как HTTP 400 с деталью из stderr:
```go
if execErr, ok := err.(*util.ExecError); ok {
    detail := execErr.Stderr  // "RTNETLINK answers: Invalid argument"
}
```

**Зависимости:** `db`, `util`, `validate`

---

### 3.7 Пакет `nat`

**Назначение:** управление Source NAT правилами (MASQUERADE/SNAT) через iptables-nft POSTROUTING.

**Ключевые типы:**

```go
type NatRule struct {
    ID, Name      string
    Source        string  // '' | CIDR | IP
    SourceAliasID string  // '' если direct CIDR/IP
    OutInterface  string
    Type          string  // "MASQUERADE" | "SNAT"
    ToSource      string  // для SNAT
    Comment       string
    Enabled       bool
    OrderIdx      int
}
```

**Публичные методы:**

| Метод | Описание |
|-------|----------|
| `New(am *aliases.Manager) *Manager` | Создать |
| `RestoreAll()` | Apply all enabled rules to kernel (FIX-13, FIX-14 idempotency) |
| `GetRules() ([]NatRule, error)` | Все правила |
| `AddRule(inp NatRuleInput) (*NatRule, error)` | Создать + apply |
| `UpdateRule(id string, inp NatRuleInput) (*NatRule, error)` | Обновить |
| `ToggleRule(id string, enabled bool) (*NatRule, error)` | Включить/выключить |
| `DeleteRule(id string) error` | Удалить + `iptables-nft -D` |
| `GetNetworkInterfaces() ([]HostInterface, error)` | `ip -o link show` (текстовый парсинг) |

**Idempotency (FIX-14):** перед каждым `-A POSTROUTING` выполняется `-C` check. Правило добавляется только если его нет. Предотвращает дубликаты при restart контейнера (`--network host` сохраняет iptables правила в ядре).

**Зависимости:** `aliases`, `db`, `util`

---

### 3.8 Пакет `gateway`

**Назначение:** CRUD шлюзов и групп шлюзов + живой мониторинг (ICMP ping + HTTP probe).

**Ключевые типы:**

```go
type Gateway struct {
    ID, Name, Interface  string
    GatewayIP            string
    MonitorAddress       string
    Enabled, Monitor     bool
    MonitorInterval      int     // ICMP probe interval, seconds
    WindowSeconds        int     // sliding window
    LatencyThreshold     int     // ms
    MonitorHttp          MonitorHttpConfig
    MonitorRule          string  // icmp_only | http_only | all | any
}

type Monitor struct {
    mu       sync.RWMutex
    states   map[string]*monitorState  // per-gateway probe data
    handlers []StatusChangeFunc        // FirewallManager callback
}

type StatusChangeFunc func(gatewayID, newStatus, prevStatus string)

type MonitorStatus struct {
    Status     string   // unknown | healthy | degraded | down
    Latency    *int     // ICMP avg ms
    PacketLoss *int     // %
    HttpStatus *string
    HttpCode   *int
}
```

**Monitor архитектура:**
- Каждый шлюз получает горутину ICMP + опционально горутину HTTP
- Sliding window: пробы старше `windowSeconds` выбрасываются
- `minProbes = 3`: минимальное число проб для перехода из `unknown`
- HTTP probe через native Go `net/http` (не curl subprocess) — избегает проблем с доступностью curl
- HTTP probe interval по умолчанию 10s (не 60s)
- StatusChange callback: вызывается **синхронно** из probe-горутины — не блокировать

**Fallback (FIX-15b):**
- При gateway `down`: если `fallbackToDefault=true` → `ip route replace default via <system-gw> table N`, иначе → `ip route replace blackhole default table N`
- При recovery: ждать 30s (anti-flap) → восстановить оригинальный маршрут
- `ip route replace` (не `ip route add`) — идемпотентно

**Зависимости:** `settings`, `util`, `validate`, `db`

---

### 3.9 Пакет `aliases`

**Назначение:** именованные наборы адресов/портов для использования в Firewall правилах.

**Типы alias:**

| Тип | Описание |
|-----|----------|
| `host` | Один или несколько IPv4 адресов |
| `network` | Один или несколько CIDR префиксов |
| `ipset` | Kernel ipset (hash:net), большие наборы; данные в `*.save` файлах |
| `group` | Объединяет несколько host/network aliases (дедупликация) |
| `port` | L4 порты: "tcp:443", "udp:53", "any:80", "tcp:8080-8090" |
| `port-group` | Объединяет несколько port aliases |

**Ключевые типы:**

```go
type Alias struct {
    ID, Name, Description string
    Type                  string   // host/network/ipset/group/port/port-group
    Entries               []string
    MemberIDs             []string // для group/port-group
    IPSetName             string   // для ipset
    EntryCount            int
    GeneratorOpts         *GeneratorOpts
    LastUpdated           string
}

type MatchSpec struct {
    Type    string    // "ipset" или "cidr"
    Name    string    // когда Type == "ipset"
    Entries []string  // когда Type == "cidr"
}
```

**Публичные методы:**

| Метод | Описание |
|-------|----------|
| `New(im *ipset.Manager) *Manager` | Создать |
| `GetAll() ([]Alias, error)` | Все aliases |
| `GetByID(id string) (*Alias, error)` | Один alias |
| `Create(data Alias) (*Alias, error)` | Создать |
| `Update(id string, upd Alias) (*Alias, error)` | Обновить |
| `Delete(id string) error` | Удалить |
| `GetMatchSpec(id string) (*MatchSpec, error)` | Получить спецификацию для iptables matching |
| `GetPortMatchSpec(id string) ([]PortMatchSpec, error)` | Спецификация для port matching |
| `UploadIPSet(id string, data []byte) error` | Загрузить данные ipset из файла |
| `StartGeneration(id string, opts GenerateOpts) (jobID string, error)` | Async генерация (RIPE) |
| `GetJobStatus(aliasID, jobID string) (*JobStatus, error)` | Статус async job |

**Зависимости:** `db`, `ipset`

---

### 3.10 Пакет `ipset`

**Назначение:** управление kernel ipset объектами (`hash:net`) для больших наборов IP-адресов.

**Особенности:**
- Restore при старте: `ipset restore -! < *.save` для каждого сохранённого ipset
- Async generation jobs в `sync.Map` (keyed by random hex jobID)
- PrefixFetcher: получает BGP префиксы из RIPE RISwhois API по стране или ASN
- CIDR агрегация: collapse overlapping prefixes

**Зависимости:** `util`

---

### 3.11 Пакет `users`

**Назначение:** multi-user аутентификация — CRUD пользователей, bcrypt пароли, TOTP secrets.

**Ключевые функции:**

| Функция | Описание |
|---------|----------|
| `List() ([]User, error)` | Все пользователи |
| `GetByID(id string) (*User, error)` | По ID |
| `GetByUsername(name string) (*User, error)` | По username (case-insensitive) |
| `Create(username, password string) (*User, error)` | Bcrypt cost 12 |
| `VerifyPassword(username, password string) (*User, error)` | Проверить пароль |
| `ChangePassword(id, newPassword string) error` | Сменить пароль |
| `SeedAdminIfEmpty(hash string) error` | Создать admin при первом запуске |
| `SetTOTPSecret(id, secret string) error` | Сохранить TOTP secret |
| `GetTOTPSecret(id string) (string, error)` | Получить для verify |
| `EnableTOTP(id string) error` | Активировать TOTP |
| `DisableTOTP(id string) error` | Деактивировать |
| `IsAdmin(id string) (bool, error)` | Проверить is_admin флаг |

**Зависимости:** `db`, `golang.org/x/crypto/bcrypt`

---

### 3.12 Пакет `tokens`

**Назначение:** API токены для программного доступа без сессии/TOTP.

**Формат токена:** `ws_` + 64 hex символа (32 random bytes = 256 bit entropy)

**Хранение:** только SHA-256(raw_token) в `api_tokens` таблице. Raw значение показывается ОДИН раз.

**Ключевые функции:**

| Функция | Описание |
|---------|----------|
| `Create(userID, name string) (*Token, string, error)` | Создать, вернуть raw token |
| `ListByUser(userID string) ([]Token, error)` | Токены пользователя |
| `VerifyAndTouch(rawToken string) (userID string, error)` | Проверить + update `last_used` |
| `Delete(id, userID string) error` | Отозвать |

**Зависимости:** `db`

---

### 3.13 Пакет `util`

**Назначение:** низкоуровневые helper'ы для всех пакетов.

**Ключевые функции и константы:**

```go
const DefaultTimeout = 30 * time.Second  // FIX-10: все нормальные операции
const FastTimeout = 5 * time.Second      // FIX-10: polling (awg show dump)

func Exec(cmd string, timeout time.Duration, log bool) (string, error)
func ExecDefault(cmd string) (string, error)   // 30s, logged
func ExecFast(cmd string) (string, error)      // 5s, logged
func ExecSilent(cmd string) (string, error)    // 30s, NOT logged (iptables -C checks)
func ExecSilentFast(cmd string) (string, error) // 5s, NOT logged

type ExecError struct {
    Err    error
    Stderr string  // FIX-15: содержит "RTNETLINK answers: ..."
    Cmd    string
}
```

**Особенности:**
- Команды выполняются через `bash -c` (поддержка process substitution: `<(...)`)
- Non-Linux (macOS dev): `Exec` всегда возвращает `("", nil)` — никаких side effects
- SIGKILL при timeout через `context.WithTimeout`

**Зависимости:** stdlib только

---

### 3.14 Пакет `validate`

**Назначение:** предотвращение command injection — все данные от пользователя, которые попадают в shell команды, валидируются перед использованием.

**Валидируемые типы:**
- WireGuard ключи (base64, 44 символа): `validate.WGKey()`
- CIDR: `validate.CIDR()` → `net.ParseCIDR`
- IP адрес: `validate.IP()`
- Interface ID (wg10, awg0): `validate.IfaceID()` → `^[a-zA-Z0-9_]{1,15}$`
- Interface name (eth0, bond-1): `validate.IfaceName()` → `^[a-zA-Z0-9_.\\-]{1,15}$`
- Routing table name: `validate.TableName()` → `^[a-zA-Z0-9_\\-]{1,31}$`
- Endpoint (host:port): `validate.Endpoint()`
- Ipset name: `validate.IpsetName()`

---

### 3.15 Пакет `awgparams`

**Назначение:** генератор AWG2 параметров обфускации (порт AmneziaWG-Architect).

**Поддерживаемые CPS-профили:**

| ID | Описание |
|----|----------|
| `random` | Случайно выбрать один из не-composite |
| `quic_initial` | QUIC Initial (RFC 9000, Long Header 0xC0-0xC3) |
| `quic_0rtt` | QUIC 0-RTT (Long Header 0xD0-0xD3) |
| `tls_client_hello` | TLS 1.3 ClientHello |
| `dtls` | DTLS 1.2 ClientHello |
| `http3` | HTTP/3 over QUIC |
| `sip` | SIP REGISTER request |
| `wireguard_noise` | WireGuard Noise_IK handshake initiation |
| `tls_to_quic` | Composite: TLS ClientHello → QUIC Initial |
| `quic_burst` | Composite: QUIC Initial → QUIC 0-RTT → HTTP/3 |

**Публичные функции:**

```go
func Generate(opts Options) (*Params, error)
func ListProfiles() []Profile
```

---

### 3.16 Пакет `settings` — ResolvePublicIP

Функция `ResolvePublicIP(mode, manual string)` (в пакете `settings`):
- `mode="manual"`: возвращает `manual` как есть
- `mode="auto"`: последовательно пробует внешние сервисы (api.ipify.org, ifconfig.me, etc.)
- Кэш: 5 минут
- Fallback: пустая строка при недоступности всех сервисов

---

## 4. Startup Sequence (порядок инициализации)

Порядок зафиксирован в `cmd/awg-easy/main.go`. Нарушение порядка вызывает тройной баг (FIX-13).

```
1. db.Init(dataDir)
   └── Открыть/создать wireguard.db
   └── Прогнать pending migrations v1..v10
   └── ОБЯЗАТЕЛЬНО первым — все пакеты зависят от db.DB()

2. api.InitAuth(passwordHash)
   └── Инициализировать session store
   └── Установить authPasswordHash для seed

3. users.SeedAdminIfEmpty(hash)
   └── Если users таблица пустая + PASSWORD_HASH задан → создать admin

4. fiber.New() + middleware (recover, request logger)
   └── Логировать только мутации (POST/PATCH/PUT/DELETE) и ошибки (4xx/5xx)
   └── GET 200 не логировать (polling каждую секунду = spam)

5. Регистрация API routes (ДО static middleware)
   └── api.RegisterAuth, RegisterCompat (без auth)
   └── apiGroup.Use(api.AuthMiddleware)  ← auth gate
   └── RegisterUsers, RegisterTokens, RegisterSettings, ...
   └── КРИТИЧНО: /api/* routes ПЕРЕД filesystem.New()
       иначе SPA fallback перехватит все API запросы

6. app.Use("/", filesystem.New(...))
   └── embed.FS с frontend (index.html, js/app.js, ...)
   └── Все неизвестные пути → index.html (SPA routing)

7. ipset.New(dataDir)
   └── Создать/проверить dataDir
   └── RestoreAll(): ipset restore -! для всех *.save файлов

8. aliases.New(ipsetMgr)
   └── Создать AliasManager с IpsetManager
   └── aliases.SetInstance(aliasMgr)

9. gateway.NewManager() + Init()
   └── Загрузить шлюзы из SQLite
   └── Запустить ICMP + HTTP probe goroutines
   └── gateway.SetInstance(gwMgr)

10. firewall.New(aliasMgr, gwMgr) + Init()
    └── initChains(): создать FIREWALL_FORWARD + FIREWALL_MANGLE цепочки
    └── rebuildChains(): flush + apply enabled rules
    └── Зарегистрировать gwMgr.Monitor().OnStatusChange callback (fallback логика)
    └── НО: applyRoutingForRule() ПАДАЕТ если wgX не существует ещё!
    └── firewall.SetInstance(fwMgr)

11. tunnel.Init(wgHost)
    ┌── Загрузить все интерфейсы из SQLite
    ├── Auto-start enabled интерфейсов (awg-quick/wg-quick up)
    │   └── FIX-2: RegenerateConfig перед каждым up
    │   └── FIX-2: down→up при "already exists" (--network host)
    └── Запустить polling goroutine (GetStatus каждые 1s)

12. fwMgr.RebuildChains()  ← ПОСЛЕ tunnel.Init (FIX-GO-9)
    └── Теперь wgX интерфейсы существуют
    └── "ip route replace default via X dev wgY table N" работает корректно
    └── PBR routing tables заполнены правильно

13. routing.New() + RestoreAll()  ← ПОСЛЕ tunnel.Init (FIX-13)
    └── "ip route add dev wgX" работает (wgX уже существует)
    └── routing.SetInstance(rmgr)

14. nat.New(aliasMgr) + RestoreAll()  ← ПОСЛЕ tunnel.Init (FIX-13)
    └── "iptables-nft -t nat -A POSTROUTING -o wgX" работает
    └── FIX-14: -C check перед -A (idempotency)
    └── nat.SetInstance(natMgr)

15. app.Listen(addr)
    └── HTTP сервер принимает запросы
```

### Почему именно такой порядок?

**Проблема A (FIX-13):** `ip route add dev wgX` — интерфейс должен существовать до добавления маршрутов.

**Проблема B (FIX-GO-9):** FirewallManager.Init() регистрирует callback и пытается создать PBR routing tables, но wgX ещё не существует → `ip route replace ... dev wgX` завершается ошибкой → таблица пустая → ip rule уже сохранён в ядре от предыдущего запуска (--network host) → пакеты маркируются → lookup пустой таблицы → fail.

**Решение:** `fwMgr.RebuildChains()` вызвать явно после `tunnel.Init()`.

### Поведение при restart контейнера

Docker `--network host` означает: WireGuard интерфейсы (`wg10`, `wg11`) **живут в хостовом ядре** и **переживают** `docker stop/start`. Следствия:

1. `awg-quick up wg10` при старте завершается ошибкой "already exists" → FIX-2: cycle down→up
2. iptables правила из предыдущего запуска сохраняются → FIX-14: -C idempotency
3. ip rule записи сохраняются → PBR таблицы нужно восстановить (FIX-GO-9)
4. Сессии (in-memory) сбрасываются — пользователи должны логиниться заново

---

## 5. API

### 5.1 Middleware порядок

```
recover.New()
→ logging middleware (только мутации + ошибки)
→ /api/*
   → RegisterAuth (без auth: /session, /auth/totp/verify)
   → RegisterCompat (без auth: /lang, /release, /remember-me, /ui-*)
   → AuthMiddleware (gate)
   → RegisterUsers, RegisterTokens, RegisterSettings
   → RegisterInterfaces, RegisterPeers, RegisterRouting
   → RegisterNat, RegisterAliases, RegisterFirewall, RegisterGateways
   → RegisterCompatAuth (с auth: /wireguard/client, /system/interfaces)
→ filesystem.New() (SPA fallback, index.html для всего остального)
```

### 5.2 Формат ответов

Фронтенд использует паттерн `res.key || []`. Go обязан оборачивать ответы:

| Эндпоинт | Обёртка |
|----------|---------|
| GET /tunnel-interfaces | `{ "interfaces": [...] }` |
| GET .../peers | `{ "peers": [...] }` |
| POST .../peers | `{ "peer": {...} }` |
| POST .../peers/import-json | `{ "peer": {...} }` |
| GET /routing/table | `{ "routes": [...] }` |
| GET /routing/tables | `{ "tables": [...] }` |
| GET /routing/routes | `{ "routes": [...] }` |
| GET /nat/interfaces | `{ "interfaces": [...] }` |
| GET /nat/rules | `{ "rules": [...] }` |
| GET /gateways | `{ "gateways": [...] }` |
| GET /gateway-groups | `{ "groups": [...] }` |
| GET /firewall/rules | голый массив `[...]` (isArray check во фронтенде) |
| GET /aliases | голый массив `[...]` |
| GET /system/interfaces | `{ "interfaces": [...] }` |

### 5.3 Полная таблица эндпоинтов

#### Auth (без AuthMiddleware)

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/session | Текущий статус сессии |
| POST | /api/session | Логин (step 1: username+password) |
| DELETE | /api/session | Logout |
| POST | /api/auth/totp/verify | Логин step 2: TOTP код |

#### Compat shims (без AuthMiddleware)

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/lang | `"en"` |
| GET | /api/release | `999999` |
| GET | /api/remember-me | `true` |
| GET | /api/ui-traffic-stats | `true` |
| GET | /api/ui-chart-type | `1` |
| GET | /api/wg-enable-one-time-links | `false` |
| GET | /api/ui-sort-clients | `false` |
| GET | /api/wg-enable-expire-time | `false` |
| GET | /api/ui-avatar-settings | `{dicebear:null,gravatar:false}` |

#### Users & Auth (с AuthMiddleware)

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/users | Список пользователей (admin only) |
| POST | /api/users | Создать пользователя (admin only) |
| GET | /api/users/me | Текущий пользователь |
| PATCH | /api/users/me | Обновить свой пароль |
| PATCH | /api/users/:id | Обновить пользователя (admin или owner) |
| DELETE | /api/users/:id | Удалить пользователя (admin или owner) |
| POST | /api/users/:id/set-admin | Выдать/забрать admin (admin only) |
| GET | /api/users/me/totp/setup | Генерировать TOTP secret + QR |
| POST | /api/users/me/totp/enable | Активировать TOTP |
| POST | /api/users/me/totp/disable | Деактивировать TOTP |

#### API Tokens

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/tokens | Токены текущего пользователя |
| POST | /api/tokens | Создать токен → `{ token: {...}, raw_token: "ws_..." }` |
| DELETE | /api/tokens/:id | Отозвать токен |

#### Settings & Templates

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/settings | Глобальные настройки + hostname + resolvedPublicIP + awgMode |
| PUT | /api/settings | Обновить настройки |
| GET | /api/templates | Список AWG2 шаблонов |
| POST | /api/templates | Создать шаблон |
| GET | /api/templates/:id | Один шаблон |
| PUT | /api/templates/:id | Обновить шаблон |
| DELETE | /api/templates/:id | Удалить |
| POST | /api/templates/:id/set-default | Установить дефолтным |
| POST | /api/templates/:id/apply | Получить params с fresh H1-H4 |
| POST | /api/templates/generate | Генерация AWG2 params `{ profile, intensity, host, saveName? }` |

#### Tunnel Interfaces

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/tunnel-interfaces | Все интерфейсы с пирами |
| POST | /api/tunnel-interfaces | Создать |
| GET | /api/tunnel-interfaces/:id | Один интерфейс |
| PATCH | /api/tunnel-interfaces/:id | Обновить (hot-reload via syncconf) |
| DELETE | /api/tunnel-interfaces/:id | Удалить |
| POST | /api/tunnel-interfaces/:id/start | Поднять |
| POST | /api/tunnel-interfaces/:id/stop | Остановить |
| POST | /api/tunnel-interfaces/:id/restart | Рестарт |
| GET | /api/tunnel-interfaces/:id/export-params | S2S interconnect export JSON |
| GET | /api/tunnel-interfaces/:id/export-obfuscation | AWG2 params JSON |
| GET | /api/tunnel-interfaces/:id/backup | Скачать interface+peers как JSON |
| PUT | /api/tunnel-interfaces/:id/restore | Восстановить пиров из JSON backup |

#### Peers

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/tunnel-interfaces/:id/peers | Список пиров |
| POST | /api/tunnel-interfaces/:id/peers | Создать пир |
| POST | /api/tunnel-interfaces/:id/peers/import-json | Interconnect import |
| GET | /api/tunnel-interfaces/:id/peers/:peerId | Один пир |
| PATCH | /api/tunnel-interfaces/:id/peers/:peerId | Обновить |
| DELETE | /api/tunnel-interfaces/:id/peers/:peerId | Удалить |
| GET | /api/tunnel-interfaces/:id/peers/:peerId/config | Скачать WG конфиг |
| GET | /api/tunnel-interfaces/:id/peers/:peerId/qrcode.svg | QR-код SVG |
| POST | /api/tunnel-interfaces/:id/peers/:peerId/enable | Включить |
| POST | /api/tunnel-interfaces/:id/peers/:peerId/disable | Выключить |
| PUT | /api/tunnel-interfaces/:id/peers/:peerId/name | Переименовать |
| PUT | /api/tunnel-interfaces/:id/peers/:peerId/address | Обновить AllowedIPs |
| PUT | /api/tunnel-interfaces/:id/peers/:peerId/expireDate | Установить/убрать expiry |
| POST | /api/tunnel-interfaces/:id/peers/:peerId/generateOneTimeLink | One-time link |
| GET | /api/tunnel-interfaces/:id/peers/:peerId/export-json | S2S peer export |

#### Routing

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/routing/table?table=main | Kernel routes (текстовый парсинг, без -j) |
| GET | /api/routing/tables | Список routing tables (rt_tables + ip rule show) |
| GET | /api/routing/test?ip=8.8.8.8[&src=X][&mark=N] | Route lookup + PBR trace |
| GET | /api/routing/routes | Статические маршруты |
| POST | /api/routing/routes | Создать |
| PATCH | /api/routing/routes/:id | Обновить / toggle `{enabled}` |
| DELETE | /api/routing/routes/:id | Удалить |

#### NAT

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/nat/interfaces | Хостовые интерфейсы (ip -o link show) |
| GET | /api/nat/rules | Список NAT правил |
| POST | /api/nat/rules | Создать |
| PATCH | /api/nat/rules/:id | Обновить / toggle |
| DELETE | /api/nat/rules/:id | Удалить |

#### Firewall

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/firewall/interfaces | Хостовые интерфейсы для rule's interface field |
| GET | /api/firewall/rules | Список правил (sorted by order) |
| POST | /api/firewall/rules | Создать |
| PATCH | /api/firewall/rules/:id | Обновить / toggle |
| DELETE | /api/firewall/rules/:id | Удалить |
| POST | /api/firewall/rules/:id/move | `{ direction: "up"\|"down" }` |

#### Gateways

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/gateways | Все шлюзы с live статусом |
| POST | /api/gateways | Создать |
| GET | /api/gateways/:id | Один шлюз |
| PATCH | /api/gateways/:id | Обновить |
| DELETE | /api/gateways/:id | Удалить |
| GET | /api/gateway-groups | Все группы |
| POST | /api/gateway-groups | Создать |
| GET | /api/gateway-groups/:id | Одна группа |
| PATCH | /api/gateway-groups/:id | Обновить |
| DELETE | /api/gateway-groups/:id | Удалить |

#### Aliases

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/aliases | Все aliases |
| POST | /api/aliases | Создать |
| GET | /api/aliases/:id | Один alias |
| PATCH | /api/aliases/:id | Обновить |
| DELETE | /api/aliases/:id | Удалить |
| POST | /api/aliases/:id/upload | Загрузить данные ipset |
| POST | /api/aliases/:id/generate | Запустить async генерацию → `{ jobId }` |
| GET | /api/aliases/:id/generate/:jobId | Статус job `{ status, entryCount?, error? }` |

#### Compat (с AuthMiddleware)

| Method | Path | Описание |
|--------|------|----------|
| GET | /api/wireguard/client | `[]` (заглушка для Admin Tunnel) |
| ALL | /api/wireguard/* | 501 Not Implemented |
| GET | /api/system/interfaces | `{ interfaces: [...] }` (для gateway form) |

---

## 6. База данных (SQLite)

### Файл

`<dataDir>/wireguard.db` (default: `/etc/wireguard/data/wireguard.db`)

### Таблицы

#### settings (key/value)

| Колонка | Тип | Описание |
|---------|-----|----------|
| key | TEXT PK | Название настройки |
| value | TEXT | Значение |

#### templates (AWG2 шаблоны)

AWG2 параметры: `id`, `name`, `is_default`, `jc`, `jmin`, `jmax`, `s1-s4`, `h1-h4`, `i1-i5`, `created_at`

#### interfaces (туннельные интерфейсы)

| Колонка | Описание |
|---------|----------|
| id | TEXT PK (wg10, wg11, ...) |
| name | Отображаемое имя |
| address | CIDR (10.8.0.1/24) |
| listen_port | UDP порт |
| protocol | wireguard-1.0 \| amneziawg-2.0 |
| enabled | 0/1 |
| disable_routes | 0/1 (Table=off) |
| private_key, public_key | WG ключи |
| jc, jmin, jmax, s1-s4, h1-h4, i1-i5 | AWG2 params (NULL для WG1) |

#### peers

| Колонка | Описание |
|---------|----------|
| id | UUID PK |
| interface_id | FK → interfaces.id (CASCADE DELETE) |
| name | Отображаемое имя |
| public_key, private_key, preshared_key | WG ключи |
| endpoint | remote endpoint host:port |
| allowed_ips | hub-side routing (10.8.0.2/32) |
| address | tunnel IP с маской (10.8.0.2/24) — для UI |
| client_allowed_ips | для клиентского конфига |
| peer_type | client \| interconnect |
| persistent_keepalive | секунды |
| enabled | 0/1 |
| expired_at | '' = no expiry |
| one_time_link | '' = нет |

#### routes (статические маршруты)

`id`, `description`, `destination`, `via`, `dev`, `metric` (nullable), `table_name`, `enabled`, `created_at`

#### nat_rules

`id`, `name`, `source`, `source_alias_id`, `out_interface`, `type`, `to_source`, `comment`, `enabled`, `order_idx`, `created_at`

#### aliases

`id`, `name`, `description`, `type`, `entries` (JSON), `member_ids` (JSON), `ipset_name`, `entry_count`, `generator_opts` (JSON), `last_updated`, `created_at`

#### firewall_rules

`id`, `name`, `enabled`, `order_idx`, `interface`, `protocol`, `source` (JSON), `destination` (JSON), `src_port`, `dst_port`, `action`, `gateway_id`, `gateway_group_id`, `fwmark` (nullable), `fallback_to_default`, `log`, `comment`, `created_at`

#### gateways

`id`, `name`, `interface`, `gateway_ip`, `monitor_address`, `enabled`, `monitor`, `monitor_interval`, `window_seconds`, `latency_threshold`, `monitor_http` (JSON), `monitor_rule`, `description`, `created_at`

#### gateway_groups

`id`, `name`, `trigger`, `description`, `members` (JSON: [{gatewayId,tier,weight}]), `created_at`

#### users (migration v7)

`id`, `username` UNIQUE NOCASE, `password_hash` (bcrypt), `totp_secret`, `totp_enabled`, `is_admin`, `created_at`

#### api_tokens (migration v8)

`id`, `user_id` FK → users.id CASCADE, `name`, `token_hash` UNIQUE (SHA-256), `last_used`, `created_at`

#### schema_migrations

`version` INTEGER PK, `applied_at`

### Что хранится в DB vs in-memory

| Данные | Хранение |
|--------|----------|
| Конфигурация всех сущностей | SQLite |
| Runtime peer stats (TransferRx/Tx, handshake, runtimeEndpoint) | In-memory (`peer.Peer` поля) |
| Async ipset generation jobs | In-memory (`sync.Map` в ipset.Manager) |
| HTTP sessions (аутентификация) | In-memory (Fiber session store) |
| Gateway probe data (ICMP/HTTP windows) | In-memory (`monitorState`) |
| Firewall fallback state | In-memory (fallbackActive, restoreTimers) |
| reloadMutex состояние | In-memory (per TunnelInterface) |

---

## 7. WireGuard/AmneziaWG управление

### Генерация конфига

Файл: `/etc/amnezia/amneziawg/<id>.conf` (mode 0600)

**PostUp/PostDown правила (FIX-1):**

```bash
# getISP — динамически находит исходящий интерфейс (eth0, ens3, etc.)
ISP=$(ip -4 route show default | awk 'NR==1{print $5}')

# PostUp:
ip link set wg10 txqueuelen 500              # снижение bufferbloat
iptables-nft -A FORWARD -i wg10 -j ACCEPT   # -A (append), НЕ -I (insert) — FIX-1
iptables-nft -A FORWARD -o wg10 -j ACCEPT   # оба направления — FIX-1
iptables-nft -t nat -A POSTROUTING -s 10.8.0.0/24 -o $ISP -j MASQUERADE

# PostDown:
iptables-nft -D FORWARD -i wg10 -j ACCEPT 2>/dev/null || true
iptables-nft -D FORWARD -o wg10 -j ACCEPT 2>/dev/null || true
iptables-nft -t nat -D POSTROUTING -s 10.8.0.0/24 -o $ISP -j MASQUERADE 2>/dev/null || true
```

**Почему `-A` (append) а не `-I` (insert):**
FirewallManager вставляет `FIREWALL_FORWARD` jump в позицию 1 при инициализации. Если PostUp использует `-I FORWARD`, wg ACCEPT-правила вставляются **перед** `FIREWALL_FORWARD` при каждом restart → весь трафик этого интерфейса обходит firewall. С `-A FORWARD` wg правила всегда ПОСЛЕ `FIREWALL_FORWARD` → firewall работает корректно.

### Протоколы

| Протокол | quickBin | syncBin |
|----------|----------|---------|
| wireguard-1.0 | wg-quick | wg |
| amneziawg-2.0 | awg-quick | awg |

### Типы пиров

| Тип | Описание | PrivateKey | AllowedIPs |
|-----|----------|-----------|-----------|
| client | Обычный клиент | хранится на сервере | `/32` (авто) |
| interconnect | S2S туннель | не хранится | `/32` или `0.0.0.0/0` |

### KernelSetPeer (FIX-9)

```
AWG 2.0:
  → Reload() (awg syncconf via process substitution)
  → Причина: awg set peer нестабилен (10-15+ секунд, оставляет ядро в плохом состоянии)

WireGuard 1.0:
  → wg set peer <pubkey> allowed-ips X endpoint Y persistent-keepalive Z
  → PSK пишется во временный файл (wg set peer preshared-key <file>)
  → Надёжен, выполняется под reloadMu
```

### KernelRemovePeer (FIX-8)

```
AWG 2.0 + kernel mode:
  → Restart() (awg-quick down + awg-quick up)
  → Причина: awg set peer remove И awg syncconf дедлочатся после нескольких add/remove
  → Минус: peer transfer stats сбрасываются

AWG 2.0 + userspace mode (WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go):
  → Reload() (awg syncconf) — стабилен в userspace daemon
  → Stats сохраняются
```

### reloadMutex

`sync.Mutex` на каждом `TunnelInterface`. Сериализует все Reload/Restart вызовы:
- Concurrent `awg syncconf + awg-quick up` = kernel deadlock
- Горутины выстраиваются в очередь → безопасное последовательное выполнение

### syncconf Timeout (FIX-GO-16)

```go
const syncconfTimeout = 10 * time.Second

func (t *TunnelInterface) doReload() error {
    cmd := fmt.Sprintf("%s syncconf %s <(%s strip %s)", ...)
    if _, err := util.Exec(cmd, syncconfTimeout, true); err != nil {
        // AWG kernel deadlock #146 — fallback на full restart
        return t.Restart()
    }
    return nil
}
```

---

## 8. Firewall (iptables-nft)

### Кастомные цепочки

```
filter table:
  FORWARD → FIREWALL_FORWARD  (jump, вставлен в позицию 1 при Init)
    FIREWALL_FORWARD содержит ACCEPT/DROP/REJECT правила по order

mangle table:
  FORWARD → FIREWALL_MANGLE   (jump, вставлен в позицию 1 при Init)
    FIREWALL_MANGLE содержит MARK правила для PBR
```

### PBR (Policy-Based Routing) поток

```
1. Пакет приходит на FORWARD
2. FIREWALL_MANGLE: если src/dst матчит PBR правило → MARK fwmark=N
3. Kernel: если пакет с fwmark=N → lookup ip rule → найти table N
4. Table N содержит: default via <gatewayIP> dev <ifaceName>
5. Пакет уходит через нужный шлюз

Компоненты:
  iptables-nft -t mangle -A FIREWALL_MANGLE -m [match] -j MARK --set-mark N
  ip rule add fwmark N table N (priority автоматически)
  ip route replace default via <gatewayIP> dev <ifaceName> onlink table N
```

**`onlink` флаг:** обходит проверку достижимости next-hop — нужно когда шлюз не в подсети интерфейса.

### Fallback при gateway down (FIX-15b)

```
Gateway status: up → down:
  fallbackToDefault=true:
    ip route replace default via <system-gw> table N
  fallbackToDefault=false:
    ip route replace blackhole default table N
  fallbackActive[ruleID] = true

Gateway status: down → up (anti-flap: ждать 30s):
  ip route replace default via <gatewayIP> dev <ifaceName> onlink table N
  fallbackActive[ruleID] = false
```

### SimulateTrace(srcIP, dstIP string)

Алгоритм (используется в `testRoute` API handler):

```
1. Получить все enabled правила, sorted by order
2. Для каждого правила:
   a. Проверить src match:
      - type=any → match
      - type=cidr → net.ParseCIDR(value).Contains(srcIP)
      - type=alias → GetMatchSpec(aliasID)
        → ipset: util.ExecSilent("ipset test <name> <ip>") exit 0 = match
        → cidr: итерировать entries, net.ParseCIDR.Contains
   b. Проверить dst match (аналогично)
   c. Invert: !match если Invert=true
3. Первое правило где src И dst matched → MatchedRule
4. Вернуть TraceResult{MatchedRule, Steps}
```

---

## 9. Routing

### Статические маршруты

Хранятся в SQLite `routes`. При `ip route add` ошибки превращаются в HTTP 400 (FIX-15):

```go
if execErr, ok := err.(*util.ExecError); ok {
    return fiber.NewError(400, "ip route: " + execErr.Stderr)
}
```

### RestoreAll() vs ReapplyForDevice()

| Функция | Когда вызывается | Что делает |
|---------|-----------------|-----------|
| `RestoreAll()` | После tunnel.Init() (startup) | Apply все enabled маршруты |
| `ReapplyForDevice(devName)` | После start/restart интерфейса | Re-add маршруты для конкретного dev |

**Почему ReapplyForDevice нужен:** `wg-quick down` удаляет ВСЕ маршруты интерфейса из ядра. После `wg-quick up` маршруты нужно восстановить.

### Запрет ip -j (FIX-11)

Флаг `-j` у iproute2 использует другой path через netlink API. На некоторых конфигурациях ядра Linux зависает навсегда. Подтверждено в production (Москва): `ip -j route show table main` зависал бесконечно.

Правило: во всём проекте **никогда** не использовать `ip -j`. Только текстовый вывод + парсинг.

### Текстовый парсинг `ip route show`

```
Формат строки: "10.8.0.0/24 dev wg10 proto kernel scope link src 10.8.0.1"
Парсинг: strings.Fields() → итерация по парам ключ-значение
```

### Таблицы маршрутизации

Обнаруживаются через:
1. `/etc/iproute2/rt_tables` (статические имена: main=254, default=253)
2. `ip rule show` (текст) — находит таблицы из хоста, например `100: from all fwmark 0x64 lookup 100`

---

## 10. NAT

### Генерируемые команды

```bash
# MASQUERADE (any source):
iptables-nft -t nat -A POSTROUTING -o eth0 -j MASQUERADE

# MASQUERADE (subnet):
iptables-nft -t nat -A POSTROUTING -s 10.8.0.0/24 -o eth0 -j MASQUERADE

# SNAT:
iptables-nft -t nat -A POSTROUTING -s 10.8.0.0/24 -o eth0 -j SNAT --to-source 1.2.3.4

# С alias source (ipset):
iptables-nft -t nat -A POSTROUTING -m set --match-set <setname> src -o eth0 -j MASQUERADE
```

### Idempotency (FIX-14)

```go
func (m *Manager) applyRule(rule *NatRule) error {
    cmd := buildIptablesCmd(rule)
    checkCmd := strings.Replace(cmd, " -A ", " -C ", 1)
    exists, _ := util.ExecSilent(checkCmd)  // exit 0 = правило есть
    if exists == "" { /* нет ошибки = правило найдено */ return nil }
    return util.ExecDefault(cmd)  // -A только если нет
}
```

### Auto-правила от tunnel interfaces

`GET /api/nat/rules` также возвращает auto-правила от tunnel interfaces (из их PostUp конфига). Они помечены как read-only в UI (badge "auto").

---

## 11. Gateways и мониторинг

### GatewayMonitor архитектура

```
Monitor {
  states: map[gatewayID] → monitorState {
    mu          sync.Mutex
    icmpProbes  []probe{ts, success, latency}
    httpProbes  []probe
    status      MonitorStatus
    stopCh      chan struct{}
  }
  handlers: []StatusChangeFunc  ← FirewallManager подписывается
}
```

### ICMP probe

```go
// Каждые monitorInterval секунд:
out, err := util.Exec(fmt.Sprintf("ping -c1 -W2 %s", addr), 5*time.Second, false)
success := err == nil
latency = parseLatencyFromPingOutput(out)
```

### HTTP probe

```go
// Каждые monitorHttp.Interval секунд (default 10s):
client := &http.Client{
    Timeout: time.Duration(monitorHttp.Timeout) * time.Second,
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
    },
}
resp, err := client.Get(monitorHttp.URL)
// Native Go http — не curl subprocess
```

### Health decision

```
MonitorRule:
  icmp_only:         здоровье = только ICMP окно
  http_only:         здоровье = только HTTP окно
  all:               здоровье = ICMP AND HTTP
  any:               здоровье = ICMP OR HTTP

Статусы:
  unknown   — меньше minProbes=3 измерений
  healthy   — successRate >= GatewayHealthyThreshold (95%)
  degraded  — successRate >= GatewayDegradedThreshold (90%)
  down      — successRate < GatewayDegradedThreshold
```

### StatusChange callback

```go
monitor.OnStatusChange(func(gwID, newStatus, prevStatus string) {
    firewall.handleGatewayStatusChange(gwID, newStatus, prevStatus)
})
// Вызывается СИНХРОННО из probe goroutine — НЕ блокировать
```

### Anti-flap (30s delay)

```go
// При down → fallback activation немедленно
// При up → запустить 30s таймер, затем восстановить оригинальный маршрут
timer := time.AfterFunc(30*time.Second, func() {
    restoreOriginalRoute(ruleID)
    fallbackActive[ruleID] = false
})
restoreTimers[ruleID] = timer
```

---

## 12. Aliases (ipset)

### Типы и matching

| Тип | MatchSpec | iptables rule |
|-----|-----------|---------------|
| host/network | `{type:"cidr", entries:["1.2.3.4","10.0.0.0/8"]}` | `-s 1.2.3.4 -s 10.0.0.0/8` (несколько правил) |
| ipset | `{type:"ipset", name:"alias_ru"}` | `-m set --match-set alias_ru src` |
| group | объединяет host/network members | как host/network дедуплицированных |
| port | `PortMatchSpec{proto,ports,multiport}` | `-p tcp --dport 443` или `--multiport` |
| port-group | объединяет port members | несколько portCombo |

### Async генерация

```
POST /api/aliases/:id/generate → { jobId: "abc123" }
  → запускает goroutine: PrefixFetcher.FetchCountry/FetchASN
  → обновляет jobs sync.Map: status = "running" → "done"|"error"

GET /api/aliases/:id/generate/:jobId
  → читает jobs sync.Map
  → если "done": FinalizeGeneration(aliasID, entryCount) → обновить DB
  → возвращает { status, entryCount?, error? }
```

**FIX-GO-3 race condition:** `watchJob` обновляет DB после фронтенд-поллинга. Исправление: `getAliasJobStatus` вызывает `FinalizeGeneration(aliasID, entryCount)` **перед** ответом — DB обновляется синхронно.

### PrefixFetcher

- RIPE RISwhois API: `https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS12345`
- Страна → RIR resources → aggregated CIDR список
- CIDR агрегация: collapse overlapping prefixes (`ip route summarize` эквивалент)

---

## 13. Caddy Reverse Proxy

Расположение: `/Users/jenya/PycharmProjects/cascade/deploy/caddy/`

### Архитектура

```
Интернет → :443 (HTTPS/HTTP3) → Caddy
  /ADMIN_PATH/* → handle_path → strip prefix → reverse_proxy 127.0.0.1:PORT → Cascade
  /ADMIN_PATH   → redirect → /ADMIN_PATH/
  * (всё остальное) → Decoy site (видео стриминг, fake landing page)

HTTP :80:
  /.well-known/acme-challenge/* → /srv/acme (webroot для cert renewal)
  * → redirect → HTTPS
```

### Security headers

```
Strict-Transport-Security: max-age=63072000; includeSubDomains; preload
X-Frame-Options: DENY
X-Content-Type-Options: nosniff
X-XSS-Protection: 1; mode=block
Referrer-Policy: no-referrer  ← скрывает ADMIN_PATH от внешних ресурсов
Permissions-Policy: geolocation=(), microphone=(), camera=()
-Server  (убирает заголовок)
-X-Powered-By
```

### КРИТИЧНО: admin off

Caddyfile содержит `admin off` — Caddy admin API отключен для безопасности.

**Следствие:** `docker exec cascade-caddy caddy reload --config /etc/caddy/Caddyfile` **не работает**.

**Reload конфига:** `docker restart cascade-caddy` — единственный способ.

**КРИТИЧНО для acme.sh:** `--reloadcmd` должен быть `docker restart cascade-caddy`, НЕ `caddy reload`.

### TLS: acme.sh shortlived cert для bare IP

**Правильный флаг:** `--cert-profile shortlived` (принимается и `--certificate-profile` — оба работают).

**RENEW_DAYS=1 обязателен:**
Shortlived cert живёт 6 дней (production) или 30 дней (staging). Default RENEW_DAYS=30 →
для 6-дневного cert NextRenewTime = expiry - 30 days = прошлое → cron обновляет каждый запуск →
rate limit LE (5 сертификатов за 7 дней на один IP). setup.sh записывает:
```bash
echo 'RENEW_DAYS=1' >> ~/.acme.sh/account.conf
# → NextRenewTime = cert_expiry - 1 день → renewal на 5-й день из 6
```

**КРИТИЧНО: НЕ использовать `--issue --force` для переключения на webroot.**
`--issue --force` выпускает новый сертификат → потребляет rate limit + перезаписывает
`Le_CertCreateTime` → `Le_NextRenewTime` пересчитывается от времени re-issue, а не от
реального истечения cert → NextRenewTime уходит в будущее → cert истекает до renewal.

**Процесс первого выпуска (setup.sh делает всё автоматически):**

Шаг 1 — standalone (Caddy ещё не запущен):
```bash
~/.acme.sh/acme.sh --issue --server letsencrypt \
    -d <PUBLIC_IP> --standalone --cert-profile shortlived
# reloadcmd игнорирует ошибку "No such container" — Caddy ещё не запущен
```

Шаг 2 — install-cert (|| true — Caddy ещё нет):
```bash
~/.acme.sh/acme.sh --install-cert -d <PUBLIC_IP> --ecc \
    --key-file /etc/ssl/cascade/server.key \
    --fullchain-file /etc/ssl/cascade/server.crt \
    --reloadcmd "docker restart cascade-caddy 2>/dev/null || true" || true
```

Шаг 3 — запустить Caddy: `docker compose up -d`

Шаг 4 — патч конфига напрямую (без выпуска нового сертификата):
```bash
CONF=~/.acme.sh/<IP>_ecc/<IP>.conf
# Переключить на webroot (НЕ --force re-issue!)
sed -i "s|^Le_Webroot=.*|Le_Webroot='/srv/acme'|" $CONF
# Пересчитать NextRenewTime от реального cert expiry
EXPIRY_UNIX=$(openssl x509 -in /etc/ssl/cascade/server.crt -noout -enddate \
  | cut -d= -f2 | xargs -I{} date -d "{}" +%s)
RENEW_AT=$((EXPIRY_UNIX - 86400))
sed -i "s|^Le_NextRenewTime=.*|Le_NextRenewTime='$RENEW_AT'|" $CONF
sed -i "s|^Le_NextRenewTimeStr=.*|Le_NextRenewTimeStr='$(date -u -d @$RENEW_AT +%Y-%m-%dT%H:%M:%SZ)'|" $CONF
```
Результат: `Le_Webroot='/srv/acme'`, `Le_NextRenewTime` = за 1 день до истечения cert.

**Renewals (webroot mode, автоматически через cron `12 1 * * *`):**
- acme.sh пишет challenge файлы в `/srv/acme`
- Caddy обслуживает `/.well-known/acme-challenge/*` из `/srv/acme` (смонтировано read-only)
- После успешного renewal: `docker restart cascade-caddy`
- Cert живёт 6 дней, renewal на 5-й день (RENEW_DAYS=1)
- Rate limit LE: 5 сертификатов за 7 дней на один IP — не превышать при тестировании

### Env переменные Caddy

| Переменная | Описание |
|------------|----------|
| `ADMIN_PATH` | Случайный сегмент пути (из .env), например `a8f3k2m9p7` |
| `CASCADE_PORT` | Порт Cascade (из .env, по умолчанию 8888) |

### BIND_ADDR

Cascade запускается с `BIND_ADDR=127.0.0.1` чтобы Web UI не был доступен напрямую:
```
--bind=127.0.0.1  # слушать только localhost
```
Только Caddy (на 127.0.0.1:PORT) может достучаться до Cascade.

---

## 14. Критические баги и фиксы

### FIX-1: iptables-nft + FORWARD в обоих направлениях + -A (append)

- **Файл:** `internal/tunnel/interface.go` → `generateWgConfig()`
- **Проблема:** Ubuntu 22.04 использует nftables. Нужны оба направления `-i` и `-o`. `-I` (insert) нарушает порядок FIREWALL_FORWARD.
- **Правильно:** `iptables-nft -A FORWARD -i wgX -j ACCEPT; iptables-nft -A FORWARD -o wgX -j ACCEPT`
- **Нельзя:** `iptables` (без -nft), только `-i` без `-o`, `-I FORWARD` вместо `-A FORWARD`

### FIX-2: RegenerateConfig перед каждым Start() + down→up при "already exists"

- **Файл:** `internal/tunnel/interface.go` → `Start()`
- **Проблема:** `--network host` → интерфейс переживает docker restart → "already exists"
- **Правильно:** RegenerateConfig() всегда перед `awg-quick up`. При "already exists" → `down` + `up`
- **Нельзя:** пропускать RegenerateConfig, или бросать ошибку при "already exists"

### FIX-3: Stop() игнорирует "not a WireGuard/AmneziaWG interface"

- **Файл:** `internal/tunnel/interface.go` → `Stop()`
- **Проблема:** Интерфейс уже был остановлен → crash
- **Правильно:** игнорировать benign errors: "is not a WireGuard interface", "is not an AmneziaWG interface", "iptables: Bad rule"

### FIX-4: Non-overlapping H1-H4 (4 зоны uint32)

- **Файл:** `internal/settings/settings.go` → `generateRandomHRanges()`
- **Проблема:** случайные числа могут пересекаться
- **Правильно:** uint32 делится на 4 равные зоны, рандомизация внутри каждой зоны

### FIX-5: Vue 2 — обновление массива только через splice

- **Файл:** `internal/frontend/www/js/app.js` → `_applyInterfaceUpdate()`
- **Проблема:** `array[idx] = newItem` не триггерит реактивность Vue 2
- **Правильно:** `this.tunnelInterfaces.splice(idx, 1, updatedIface)`

### FIX-6: Address пира вычисляется из AllowedIPs + маска интерфейса

- **Файл:** `internal/peer/peer.go` → `GenerateRemoteConfig()`
- **Проблема:** поле `remoteAddress` удалено
- **Правильно:** `peerIP = AllowedIPs.split('/')[0]`; `Address = peerIP + '/' + ifaceMask`

### FIX-7: quickBin/syncBin по протоколу

- **Файл:** `internal/tunnel/interface.go`
- **Проблема:** захардкоженный `awg-quick` или `wg-quick`
- **Правильно:** `amneziawg-2.0` → `awg-quick`/`awg`, `wireguard-1.0` → `wg-quick`/`wg`

### FIX-8: KernelRemovePeer — AWG2 использует Restart()

- **Файл:** `internal/tunnel/interface.go` → `KernelRemovePeer()`
- **Проблема:** `awg set peer remove` дедлочится в AWG kernel module
- **Правильно:** kernel mode → `Restart()` под reloadMu; userspace mode → `Reload()`

### FIX-9: KernelSetPeer — AWG2 использует syncconf (Reload)

- **Файл:** `internal/tunnel/interface.go` → `KernelSetPeer()`
- **Проблема:** `awg set peer` нестабилен (10-15+ секунд, плохое состояние ядра)
- **Правильно:** AWG2 → `Reload()` (syncconf); WG1 → `wg set peer` (надёжен)

### FIX-10: Util.Exec — timeout по умолчанию 30s, getStatus 5s

- **Файл:** `internal/util/exec.go`
- **Проблема:** без timeout зависший `awg`/`wg` накапливается → container freeze
- **Правильно:** DefaultTimeout=30s, FastTimeout=5s, SIGKILL при превышении

### FIX-11: ip -j ЗАПРЕЩЁН

- **Файл:** `internal/routing/manager.go`, `internal/nat/manager.go`
- **Проблема:** `ip -j` зависает навсегда на некоторых ядрах Linux
- **Правильно:** только текстовый вывод + парсинг (strings.Fields, bufio.Scanner)

### FIX-12: HTTP method uppercase (Node.js 22 llhttp)

- **Файл:** `internal/frontend/www/js/api.js`
- **Проблема:** `fetch(..., { method: 'patch' })` → llhttp отвергает с 400 + TCP RST
- **Правильно:** `method: method.toUpperCase()` — один раз покрывает все вызовы

### FIX-13: Порядок инициализации — InterfaceManager ПЕРВЫМ

- **Файл:** `cmd/awg-easy/main.go`
- **Проблема:** три бага: (A) `ip route add dev wgX` падает если wgX не существует; (B) routing параллельно с tunnel → маршруты добавлены, потом tunnel делает down→up → маршруты удалены; (C) chain в начале конструктора не работал
- **Правильно:** tunnel.Init → fwMgr.RebuildChains → routing.RestoreAll → nat.RestoreAll

### FIX-14: NAT idempotency через iptables -C

- **Файл:** `internal/nat/manager.go` → `applyRule()`
- **Проблема:** при restart контейнера дублирующие POSTROUTING правила
- **Правильно:** `-C` check перед каждым `-A`; добавлять только если нет

### FIX-15: ip route ошибки → HTTP 400 с деталью из stderr

- **Файл:** `internal/routing/manager.go`
- **Проблема:** без обёртки возвращался 500 без полезного сообщения
- **Правильно:** `*util.ExecError` → `fiber.NewError(400, "ip route: " + execErr.Stderr)`

### FIX-15b: Gateway fallback — blackhole/default route при gateway down

- **Файлы:** `internal/gateway/monitor.go`, `internal/firewall/manager.go`
- **Проблема:** PBR правило с gateway не реагировало на падение — трафик уходил в никуда
- **Правильно:** `ip route replace blackhole default table N` или `ip route replace default via <sys-gw>` + 30s anti-flap restore

### FIX-GO-8: Route test — "ip route get from <non-local>" → Network unreachable

- **Файл:** `internal/api/routing.go` → `testRoute()`
- **Проблема:** `ip route get <dst> from <src>` где src не принадлежит локальному интерфейсу → "RTNETLINK answers: Network unreachable"
- **Правильно:** SimulateTrace(src, dst) → найти fwmark → `ip route get <dst> mark <fwmark>`

### FIX-GO-9: PBR routing table пустой после restart контейнера

- **Файлы:** `cmd/awg-easy/main.go`, `internal/api/interfaces.go`
- **Проблема:** firewall.Init() вызывался до tunnel.Init() → `ip route replace ... dev wgX` падал (wgX не существует)
- **Правильно:** `fwMgr.RebuildChains()` вызывать явно после `tunnel.Init()`. В start/restart handlers: `firewall.Get().RebuildChains()`. `onlink` в ip route replace.

### FIX-GO-10: ipInCIDR всегда false для не-network-address IP

- **Файл:** `internal/firewall/manager.go`
- **Проблема:** `bits.RotateLeft32(^uint32(0), -prefixLen)` всегда возвращал `0xFFFFFFFF` → маска не применялась
- **Правильно:** `net.ParseCIDR(cidr)` + `ipNet.Contains(ip)` (stdlib)

### FIX-GO-11: importPeerJSON не сохранял address пира

- **Файл:** `internal/api/peers.go` → `importPeerJSON()`
- **Проблема:** address не писался в inp.Address → пустой address у всех interconnect пиров
- **Правильно:** явно читать `body["address"]` в `inp.Address` до обработки allowedIPs

### FIX-GO-13: GetAllPeers — нестабильный порядок из Go map

- **Файл:** `internal/tunnel/interface.go` → `GetAllPeers()` и `internal/tunnel/manager.go` → `GetAllPeers()`
- **Проблема:** Go map iteration non-deterministic → dashboard перетасовывал карточки каждую секунду
- **Правильно:** `sort.Slice(out, func(i,j int) bool { return out[i].CreatedAt < out[j].CreatedAt })`

### FIX-GO-16: doReload() — 10s timeout + fallback на Restart() при deadlock syncconf

- **Файл:** `internal/tunnel/interface.go` → `doReload()`
- **Проблема:** AWG kernel deadlock #146 — syncconf зависает навсегда
- **Правильно:** `util.Exec(cmd, 10*time.Second, true)` + при ошибке → `t.Restart()`

---

## 15. Frontend архитектура

### Технологии

- Vue 2 (CDN, без webpack/build step) — все файлы в `internal/frontend/www/`
- Tailwind CSS (CDN, prекомпилированный статический `app.css`)
- VueI18n для локализации
- ApexCharts для графиков трафика
- embed.FS: весь frontend вшит в бинарник при компиляции (`//go:embed all:www`)

### ВАЖНО: файлы для редактирования

Фронтенд для Go rewrite находится в `internal/frontend/www/`, **НЕ** в `src/www/`. Изменения в `src/www/` не попадают в Go-бинарник.

### Главные файлы

| Файл | Описание |
|------|----------|
| `internal/frontend/www/index.html` | Весь UI (один файл, Vue template) |
| `internal/frontend/www/js/app.js` | Vue app: data + methods + polling |
| `internal/frontend/www/js/api.js` | Все API методы |
| `internal/frontend/www/css/app.css` | Prекомпилированный Tailwind CSS |

### api.js — метод call()

FIX-12: все HTTP методы должны быть uppercase (Node.js 22 llhttp):
```javascript
async call({ method, path, body }) {
    const res = await fetch(`./api${path}`, {
        method: method.toUpperCase(),  // Node.js 22 llhttp: HTTP method must be uppercase
        headers: { 'Content-Type': 'application/json' },
        body: body ? JSON.stringify(body) : undefined,
    });
}
```

### Polling

```javascript
setInterval(() => {
    if (activePage === 'interfaces') {
        if (activeInterfaceId) refreshPeers();
        else refreshAllPeers();  // dashboard mode
    }
}, 1000);
```

### Reactive updates (Vue 2)

FIX-5: `array[i] = value` не триггерит реактивность Vue 2. Использовать `splice`:
```javascript
_applyInterfaceUpdate(updatedIface) {
    const idx = this.tunnelInterfaces.findIndex(i => i.id === updatedIface.id);
    if (idx !== -1) {
        this.tunnelInterfaces.splice(idx, 1, updatedIface);
    }
}
```

### Страницы (activePage)

| activePage | Описание |
|-----------|----------|
| `interfaces` | Interfaces: All (dashboard) + per-interface tabs |
| `gateways` | Gateways + Gateway Groups |
| `routing` | Routing: Status / Static / OSPF |
| `nat` | NAT: Outbound / Port Forwarding |
| `firewall-aliases` | Firewall → Aliases |
| `firewall` | Firewall → Rules |
| `settings` | Global Settings + AWG2 Templates |
| `administration` | Admin Tunnel (Legacy wg0) |

### Tailwind CSS ограничения

`app.css` — **prекомпилированный статический файл**. Новые Tailwind классы не работают.

Перед использованием любого класса проверить его наличие в `app.css`. Отсутствующие классы заменять `style="..."` (inline CSS).

**Известные отсутствующие классы:** `px-6`, `py-10`, `py-8`, `min-h-full`, `items-start`, `p-6`, `border-t`, `border-neutral-*`, `space-y-2`, `space-y-4`, `space-y-6`, `hover:bg-*`.

---

## 16. Правила разработки

### ПРАВИЛО 0: При изменении API обновить документацию

При добавлении, изменении или удалении **любого API endpoint** обновить оба файла:
- `docs/API.md` (русская версия)
- `docs/API.en.md` (английская версия)

Оба файла должны попасть в **один коммит** вместе с изменениями кода.

### ПРАВИЛО 1: Читать файл целиком перед редактированием

Всегда читать файл через Read tool перед точечным изменением через Edit. Никогда не писать код "из памяти". Это предотвращает случайное уничтожение уже исправленных багов.

### ПРАВИЛО 2: Tailwind CSS — только существующие классы

Проверять наличие класса в `app.css` перед использованием. Использовать `style="..."` для отсутствующих классов.

### Правило коммитов

Каждый коммит — подробное сообщение (что, почему, какие файлы). Формат:
```
feat/fix/refactor(компонент): краткое описание

- детальное описание изменения 1
- детальное описание изменения 2
- файлы: internal/..., cmd/..., docs/...
```

### Правило деплоя

Коммитить и пушить только в `master` (Go rewrite ветка).

```bash
git pull origin master
./build-go.sh
docker compose -f docker-compose.go.yml down && docker compose -f docker-compose.go.yml up -d
```

### Никогда не изменять

- Существующие migrations в `internal/db/db.go` — только добавлять новые
- Порядок инициализации в `cmd/awg-easy/main.go` без понимания FIX-13/FIX-GO-9

---

## 17. Деплой

### Docker режим (текущий: host network)

```yaml
# docker-compose.go.yml
services:
  cascade:
    image: ghcr.io/johnnyvbut/cascade:latest
    container_name: cascade
    network_mode: host     # обязательно для WG/AWG + iptables
    cap_add:
      - NET_ADMIN
      - SYS_MODULE          # для kernel module AWG; не нужен в userspace mode
    devices:
      - /dev/net/tun:/dev/net/tun
    volumes:
      - ./data:/etc/wireguard/data
      - /etc/hostname:/host_hostname:ro   # hostname для UI
```

### Переменные окружения

| Переменная | Default | Описание |
|------------|---------|----------|
| `DATA_DIR` | `/etc/wireguard/data` | Путь к данным |
| `PORT` | `8888` | Web UI TCP порт |
| `BIND_ADDR` | `""` (0.0.0.0) | Хост для listen (127.0.0.1 за reverse proxy) |
| `WG_PORT` | `555` | Default UDP порт для новых интерфейсов |
| `WG_HOST` | `""` | Публичный IP/hostname (опционально, можно в UI) |
| `PASSWORD_HASH` | `""` | bcrypt хэш для seed admin пользователя |
| `DEBUG` | `false` | Debug logging |
| `WG_QUICK_USERSPACE_IMPLEMENTATION` | `""` | Установить `amneziawg-go` для userspace mode |

### Dockerfile multi-stage

```
Stage 1 (builder): golang:1.23-alpine
  CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" ./cmd/awg-easy
  Результат: статический бинарник, нет зависимостей от libc

Stage 2 (runtime): amneziavpn/amneziawg-go:latest
  Добавляет: dumb-init, iptables-legacy, iproute2, ipset
  iptables-legacy как default (symlinks)
  Копирует бинарник + entrypoint.sh
```

### Режимы AWG

| Режим | `WG_QUICK_USERSPACE_IMPLEMENTATION` | Описание |
|-------|-------------------------------------|----------|
| kernel | `""` (не задан) | AWG kernel module, требует `SYS_MODULE`, KernelRemovePeer → Restart |
| userspace | `amneziawg-go` | AWG userspace daemon, стабильнее, KernelRemovePeer → Reload |

### Команды деплоя

```bash
# Первоначальный деплой
./deploy/setup.sh

# Обновление
git pull origin master
docker compose -f docker-compose.go.yml pull   # или ./build-go.sh для локальной сборки
docker compose -f docker-compose.go.yml up -d

# Caddy (TLS reverse proxy)
cd deploy/caddy
docker compose up -d

# Первый TLS сертификат
sudo ./scripts/acme-install.sh <PUBLIC_IP> <EMAIL>
docker compose up -d  # запустить Caddy после получения сертификата

# Просмотр логов
docker logs -f cascade
docker logs -f cascade-caddy
```

---

## Обновление этого документа

При добавлении/изменении/удалении любой фичи обновить этот документ в том же коммите. Это не опционально.

Новый разработчик или следующая сессия Claude должны прочитать ТОЛЬКО этот файл и понять всё устройство системы без дополнительных вопросов.

**Что обновлять при изменении:**
- Новый пакет → раздел 3 (Backend пакеты)
- Новый API endpoint → раздел 5.3 (таблица эндпоинтов) + `docs/API.md` + `docs/API.en.md`
- Новая миграция DB → раздел 6 (список таблиц)
- Новый критический баг-фикс → раздел 14 (FIX-N)
- Изменение startup sequence → раздел 4
- Новая переменная окружения → раздел 17
