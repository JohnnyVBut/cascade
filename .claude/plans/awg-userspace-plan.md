# AWG Userspace Migration Plan

Branch: `feature/awg-userspace` (created from `feature/go-rewrite`)
Date: 2026-03-24
Author: architect review

---

## 1. Текущее состояние — как работают туннели

### 1.1 Бинарники и команды

Код в `internal/tunnel/interface.go` использует две пары бинарников, выбираемых по `Protocol`:

| Protocol | quickBin | syncBin |
|---|---|---|
| `wireguard-1.0` | `wg-quick` | `wg` |
| `amneziawg-2.0` | `awg-quick` | `awg` |

Фактические shell-вызовы:

| Операция | Команда |
|---|---|
| Start | `awg-quick up <id>` |
| Stop | `awg-quick down <id>` |
| Reload (hot) | `awg syncconf <id> <(awg-quick strip <id>)` |
| Status | `awg show <id> dump` |
| Keygen | `awg genkey`, `echo priv \| awg pubkey`, `awg genpsk` |
| Peer set (WG1) | `wg set <id> peer <pubkey> allowed-ips ... endpoint ...` |

### 1.2 Директория конфигов

`const confDir = "/etc/amnezia/amneziawg"` — именно туда пишутся `.conf` файлы перед `awg-quick up`.

### 1.3 Парсинг вывода `awg show dump`

Формат строк tab-separated (тот же для ядерного и userspace режима):
```
<interface_row>
<pubkey>\t<preshared>\t<endpoint>\t<allowed_ips>\t<latest_handshake_ts>\t<rx>\t<tx>\t<keepalive>
```

Поля с индексами 0 (pubkey), 2 (endpoint), 4 (handshake ts), 5 (rx), 6 (tx) читаются в `GetStatus()`.

### 1.4 Serialization (mutex)

`reloadMu sync.Mutex` — сериализует `Reload()` и `KernelRemovePeer()` чтобы не было concurrent `syncconf` (известный deadlock AWG kernel module).

### 1.5 Lifecycle (FIX-2, FIX-3)

- `Start()`: всегда `RegenerateConfig()` перед `awg-quick up`; при "already exists" — cycle down→up; при "Address in use" — flush stale route → retry.
- `Stop()`: игнорирует "not a WireGuard/AmneziaWG interface" и iptables benign errors.
- `doReload()`: `syncconf` с таймаутом 10s; при ошибке — fallback на `Restart()`.

---

## 2. Что такое amnezia-wg-go и как он запускается

### 2.1 Репозиторий

`github.com/amnezia-vpn/amneziawg-go` — Go userspace реализация AmneziaWG (форк `wireguard-go`).

### 2.2 Docker-образ `amneziavpn/amneziawg-go:latest`

Исследован Dockerfile из репозитория (тег v0.2.16, дата 2025-12-01):

```dockerfile
FROM golang:1.24.4 as awg
COPY . /awg
WORKDIR /awg
RUN go build -o /usr/bin

FROM alpine:3.19
ARG AWGTOOLS_RELEASE="1.0.20250901"

RUN apk --no-cache add iproute2 iptables bash && \
    cd /usr/bin/ && \
    wget https://github.com/amnezia-vpn/amneziawg-tools/releases/download/v${AWGTOOLS_RELEASE}/alpine-3.19-amneziawg-tools.zip && \
    unzip -j alpine-3.19-amneziawg-tools.zip && \
    chmod +x /usr/bin/awg /usr/bin/awg-quick && \
    ln -s /usr/bin/awg /usr/bin/wg && \
    ln -s /usr/bin/awg-quick /usr/bin/wg-quick
COPY --from=awg /usr/bin/amneziawg-go /usr/bin/amneziawg-go
```

**Критические наблюдения:**

1. Образ содержит `amneziawg-go` (userspace daemon) + `awg-quick` + `awg` (из amneziawg-tools). Инструменты (`awg`, `awg-quick`) — те же самые, независимо от kernel/userspace.
2. `awg-quick up` работает И с kernel module, И с userspace daemon. Способ выбора — переменная окружения `WG_QUICK_USERSPACE_IMPLEMENTATION`.
3. Текущий `Dockerfile.go` уже использует этот образ как базовый (`FROM amneziavpn/amneziawg-go:latest`).
4. `wg` и `wg-quick` — симлинки на `awg` и `awg-quick`.

### 2.3 Как запускается userspace режим

`awg-quick` поддерживает userspace через переменную окружения:
```bash
WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go awg-quick up wg10
```

Это заставляет `awg-quick` запустить `amneziawg-go` процесс вместо загрузки kernel module.

Daemon `amneziawg-go` создаёт TUN-устройство через `/dev/net/tun` и UNIX socket в `/var/run/wireguard/<iface>.sock`. Команды `awg show dump` и `awg syncconf` общаются с daemon через этот socket.

### 2.4 Ключевые отличия userspace vs kernel

| Аспект | Kernel module | Userspace (amneziawg-go) |
|---|---|---|
| Требует PPA + modprobe | Да | Нет |
| Kernel version constraint | Да (≥5.x, HWE для 22.04) | Нет |
| Нужен /dev/net/tun | Нет | Да |
| SYS_MODULE capability | Да | Нет |
| NET_ADMIN capability | Да | Да |
| deadlock в syncconf | Да (kernel #146) | Нет (userspace стабилен) |
| awg show dump формат | Одинаковый | Одинаковый |
| awg-quick up/down | Одинаковый | Одинаковый (другая реализация) |
| Производительность | 100% | ~50-70% на загруженных узлах |
| Типичное использование | Продакшн с высоким трафиком | VPN-маршрутизация, управление |

---

## 3. Анализ изменений по файлам

### 3.1 `Dockerfile.go` — ИЗМЕНИТЬ (medium)

**Текущее состояние:**
```dockerfile
FROM amneziavpn/amneziawg-go:latest
```
Образ уже содержит `amneziawg-go` бинарник. Изменений в FROM строке не нужно.

**Что нужно добавить:**
- Переменную окружения `WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go` в `ENV` или в запуск daemon.

**Что нужно проверить/убрать:**
- Текущий `CMD`: `["/usr/bin/dumb-init", "cascade", ...]` — запускает только Go-приложение. `amneziawg-go` daemon запускается отдельно per-interface через `awg-quick up` с env переменной. Это правильно.
- `cap_add: SYS_MODULE` в docker-compose — нужно убрать (userspace не требует загрузки модуля ядра).
- Healthcheck: `wg show | grep -q interface` — будет работать и с userspace (awg-quick создаёт интерфейс).

**Нет необходимости:**
- Менять базовый образ. `amneziavpn/amneziawg-go:latest` уже содержит нужный бинарник.

### 3.2 `docker-compose.go.yml` — ИЗМЕНИТЬ (small)

**Текущее:**
```yaml
cap_add:
  - NET_ADMIN
  - SYS_MODULE
devices:
  - /dev/net/tun:/dev/net/tun
```

**Нужно:**
- Убрать `SYS_MODULE` — userspace не требует `modprobe amneziawg`.
- `NET_ADMIN` — оставить (нужен для TUN device и iptables).
- `/dev/net/tun` — оставить (нужен для TUN device userspace режима).

**Добавить environment:**
```yaml
environment:
  - WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go
```

### 3.3 `internal/tunnel/interface.go` — ИЗМЕНИТЬ (small-medium)

**Deadlock-специфичные fallback-ы:**

Текущий код содержит `syncconfTimeout = 10*time.Second` и fallback на `Restart()` в `doReload()`, потому что AWG kernel module deadlocks при `syncconf`. В userspace этого нет.

Рекомендация: оставить timeout и fallback — они не вредят userspace, а только добавляют защиту. Удалять их было бы регрессией если кто-то вернётся к kernel mode.

**`KernelRemovePeer()`:**

Текущий комментарий: "Both `awg set peer remove` and `awg syncconf` deadlock in the AWG kernel module". В userspace `awg syncconf` стабилен — `awg set peer remove` тоже. Можно заменить `Restart()` на `Reload()` (syncconf) для remove. Это ускорит операцию удаления пира (нет down→up цикла, статы трафика сохраняются).

**`confDir = "/etc/amnezia/amneziawg"`:**

Это путь где `awg-quick` ищет конфиги. В образе `amneziavpn/amneziawg-go` путь тот же. Менять не нужно.

**`quickBin()` / `syncBin()`:**

Без изменений — env переменная `WG_QUICK_USERSPACE_IMPLEMENTATION` работает прозрачно для `awg-quick`.

**`Start()` — "already exists" handling:**

В userspace режиме `amneziawg-go` процесс может остаться живым после `docker stop` (через `--network host` интерфейс выживает). Существующая логика down→up при "already exists" правильна и для userspace. Менять не нужно.

### 3.4 `deploy/setup.sh` — ИЗМЕНИТЬ (medium)

**Секция STEP 2 — полностью убирается:**
```bash
# ── Step 2: AmneziaWG kernel module ──────
if lsmod | grep -q amneziawg; then
  ok "amneziawg already loaded"
else
  info "Installing AmneziaWG..."
  add-apt-repository -y ppa:amnezia/ppa > /dev/null 2>&1
  apt-get update -qq
  apt-get install -y amneziawg
  modprobe amneziawg
  echo "amneziawg" > /etc/modules-load.d/amneziawg.conf
  ok "amneziawg installed and loaded"
fi
```

**Секция STEP 1 — может быть упрощена:**
Требование HWE 6.x kernel для Ubuntu 22.04 обусловлено именно kernel module. С userspace kernel ограничений нет — достаточно 5.10+ (нужен для TUN device поддержки, которая есть с Linux 3.x). STEP 1 можно удалить или сделать необязательным (no HWE requirement).

**Добавить в sysctl (STEP 4):**
Значения `net.core.rmem_max` и `net.core.wmem_max` уже есть. Для userspace могут помочь, но не критичны.

**Изменить текст summary:**
Убрать упоминание kernel module.

### 3.5 `go.mod` — НЕ ИЗМЕНЯТЬ

Никаких новых Go-зависимостей не требуется. Все операции остаются через `exec.Command("bash", "-c", cmd)` с теми же бинарниками.

---

## 4. Пошаговый план реализации

### Шаг 1 — docker-compose.go.yml: убрать SYS_MODULE, добавить env (small)

Файл: `/Users/jenya/PycharmProjects/cascade/docker-compose.go.yml`

- Убрать `- SYS_MODULE` из `cap_add`
- Добавить `- WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go` в `environment`
- `NET_ADMIN` и `/dev/net/tun` — оставить без изменений

### Шаг 2 — Dockerfile.go: убрать iptables-legacy, добавить ENV (small)

Файл: `/Users/jenya/PycharmProjects/cascade/Dockerfile.go`

- Проверить: нужен ли `iptables-legacy` в userspace режиме. Текущий код использует `iptables-nft` в PostUp/PostDown. Симлинки на iptables-legacy могут конфликтовать с iptables-nft, если образ уже настроен под nft. Это нужно исследовать отдельно.
- Добавить `ENV WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go` — чтобы переменная была в контейнере по умолчанию.

### Шаг 3 — setup.sh: удалить STEP 2 (kernel module), упростить STEP 1 (small)

Файл: `/Users/jenya/PycharmProjects/cascade/deploy/setup.sh`

- STEP 2 (AmneziaWG kernel module) — полностью удалить
- STEP 1 (kernel upgrade) — сделать optional или удалить; минимальное требование — kernel ≥ 5.10 для TUN (уже гарантировано на Ubuntu 22.04 даже без HWE)
- Обновить шаги нумерацию (Step 3 → Step 2, etc.)
- Обновить summary text

### Шаг 4 — internal/tunnel/interface.go: оптимизировать KernelRemovePeer (medium)

Файл: `/Users/jenya/PycharmProjects/cascade/internal/tunnel/interface.go`

**Изменение:** `KernelRemovePeer()` — заменить `Restart()` на `Reload()` для протокола `amneziawg-2.0` (так же как `KernelSetPeer` использует `Reload()` для AWG2).

Обоснование: deadlock при `awg syncconf` — это проблема исключительно kernel module. В userspace `syncconf` стабилен. Restart сбрасывает трафик-статистику пиров без необходимости.

**Добавить комментарий** в `doReload()` и `syncconfTimeout` что timeout оставлен как защита, но deadlock-специфичен для kernel module.

**Не менять:** `syncconfTimeout = 10s` и fallback Restart() в `doReload()` — harm-free в userspace.

### Шаг 5 — Обновить тесты tunnel_test.go (small)

Файл: `/Users/jenya/PycharmProjects/cascade/internal/tunnel/tunnel_test.go`

Текущие тесты покрывают `quickBin()` и `syncBin()`. При изменении `KernelRemovePeer()`:
- Добавить тест что `KernelRemovePeer()` для AWG2 вызывает `Reload()`, а не `Restart()`.

---

## 5. Риски и сложности

### Риск 1 (HIGH): iptables-nft vs iptables-legacy в образе

**Проблема:** `Dockerfile.go` устанавливает `iptables-legacy` и делает симлинки:
```dockerfile
RUN ln -sf /sbin/iptables-legacy /sbin/iptables && ...
```

Но код в `generateWgConfig()` использует `iptables-nft` явно в PostUp/PostDown.

При userspace режиме `awg-quick up` выполняет PostUp скрипт. Если в образе `amneziavpn/amneziawg-go` уже настроен nftables, а Dockerfile.go переключает на legacy, может быть конфликт.

**Решение до реализации:** нужно проверить что находится в образе `amneziavpn/amneziawg-go:latest` — какая версия iptables там стоит по умолчанию. Dockerfile образа (`FROM alpine:3.19`) устанавливает просто `iptables` без explicit legacy/nft. Alpine 3.19 по умолчанию использует iptables-nft.

**Действие:** убрать iptables-legacy симлинки из `Dockerfile.go` и оставить только `iptables` (который в alpine 3.19 это nft-backend). PostUp в конфиге явно использует `iptables-nft` — это совместимо.

### Риск 2 (MEDIUM): Жизненный цикл amneziawg-go процесса

**Проблема:** В userspace режиме каждый интерфейс — отдельный `amneziawg-go` процесс. При `awg-quick down` процесс должен завершиться. При `docker stop` контейнера с `--network host` TUN-интерфейс удаляется (в отличие от kernel interface). Это хорошая новость — нет "already exists" при перезапуске контейнера.

НО: если `amneziawg-go` процесс зависнет (при kernel panic etc.), `awg-quick up` не сможет создать новый интерфейс.

Существующий код в `Start()` обрабатывает "already exists" через down→up. Этот случай менее вероятен при userspace, но код можно оставить.

### Риск 3 (MEDIUM): /var/run/wireguard/<iface>.sock доступность

**Проблема:** `awg show dump` и `awg syncconf` используют UNIX socket для общения с userspace процессом. Если socket не существует (процесс не запущен), команда немедленно вернёт ошибку вместо зависания.

Существующий timeout 5s в `ExecSilentFast` (для `GetStatus()`) и 10s в `doReload()` (для syncconf) — адекватны. В userspace ошибка придёт быстро.

### Риск 4 (LOW): Производительность при высоком трафике

**Контекст:** userspace работает с ~50-70% от kernel throughput. Для маршрутизатора с многими клиентами это может быть заметно при суммарном трафике >1 Gbps. Для типичного VPN-сервера с несколькими десятками клиентов разница несущественна.

### Риск 5 (LOW): Совместимость конфиг-формата

Формат `.conf` файла (AWG2 параметры: Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5) одинаков для kernel module и userspace. `awg-quick` парсит один и тот же формат. Изменений в `generateWgConfig()` не нужно.

### Риск 6 (LOW): Откат к kernel module

Если нужно вернуться к kernel mode — достаточно убрать `WG_QUICK_USERSPACE_IMPLEMENTATION` env переменную и добавить `SYS_MODULE`. Никаких изменений в Go коде не потребуется (если KernelRemovePeer изменён — нужно проверить что работает с обоими режимами).

---

## 6. Нужны ли изменения в API

**Нет.** API полностью не меняется.

Все операции через API (`/api/tunnel-interfaces/:id/start`, `/stop`, `/restart`, etc.) вызывают те же методы (`Start()`, `Stop()`, `Restart()`, `KernelSetPeer()`, `KernelRemovePeer()`). Изменение с kernel на userspace прозрачно для API-слоя.

Единственное потенциальное изменение — в ответе `GET /api/tunnel-interfaces/:id` можно добавить информацию о режиме (kernel/userspace), но это feature, не requirement.

---

## 7. Файлы, которые НЕ нужно трогать

| Файл | Причина |
|---|---|
| `internal/tunnel/manager.go` | Бизнес-логика не меняется |
| `internal/peer/peer.go` | Без изменений |
| `internal/api/*.go` | API контракт не меняется |
| `internal/firewall/manager.go` | iptables управление не меняется |
| `internal/nat/manager.go` | NAT правила не меняются |
| `internal/routing/manager.go` | Маршруты не меняются |
| `go.mod` / `go.sum` | Новых зависимостей нет |
| `internal/frontend/www/` | UI не меняется |
| `deploy/caddy/` | Caddy конфиг не связан с AWG |

---

## 8. Обратная совместимость

- **База данных (SQLite):** без изменений. Protocol `"amneziawg-2.0"` и `"wireguard-1.0"` сохраняются в том же формате.
- **Конфиг-файлы** (`/etc/amnezia/amneziawg/*.conf`): формат не меняется.
- **Существующие туннели:** при обновлении контейнера с добавлением `WG_QUICK_USERSPACE_IMPLEMENTATION`, интерфейсы с `enabled=true` поднимутся в userspace режиме автоматически через `tunnel.Init()` → `t.Start()`.
- **Клиентские конфиги (QR/download):** без изменений — формат клиентского WireGuard конфига идентичен.

---

## 9. Оценка сложности

| Шаг | Файл | Сложность |
|---|---|---|
| 1 | docker-compose.go.yml | small |
| 2 | Dockerfile.go | small |
| 3 | deploy/setup.sh | small |
| 4 | internal/tunnel/interface.go | medium |
| 5 | internal/tunnel/tunnel_test.go | small |

**Итого:** ~2-4 часа работы. Основная сложность — понять правильное место для `WG_QUICK_USERSPACE_IMPLEMENTATION` (ENV в Dockerfile vs docker-compose) и проверить iptables-legacy вопрос.

---

## 10. Рекомендуемый порядок коммитов

1. `chore(docker): remove SYS_MODULE, add userspace env — docker-compose.go.yml + Dockerfile.go`
2. `fix(setup): remove kernel module installation step — deploy/setup.sh`
3. `fix(tunnel): KernelRemovePeer uses Reload() for AWG2 in userspace mode`
4. `test(tunnel): add KernelRemovePeer reload assertion for userspace`
5. `docs: update CLAUDE.md checkpoint — awg-userspace branch`

---

## 11. Вопросы для проверки перед реализацией

1. **iptables в образе amneziavpn/amneziawg-go**: нужно подтвердить что alpine 3.19 там использует nftables backend (ожидаемо, но нужно проверить `docker run amneziavpn/amneziawg-go:latest iptables --version`). Если там iptables-legacy — симлинки из Dockerfile.go корректны и их нужно сохранить.

2. **WG_QUICK_USERSPACE_IMPLEMENTATION scope**: переменная должна быть доступна не только процессу `cascade`, но и shell, который `cascade` запускает через `bash -c`. Поскольку `util.Exec()` делает `exec.Command("bash", "-c", cmd)`, а ENV наследуется дочерним процессом, установка переменной в `ENV` слое Dockerfile или в docker-compose `environment` достаточна.

3. **Тест с реальным интерфейсом**: после реализации — создать тестовый интерфейс, проверить что в `docker ps` видно отдельный `amneziawg-go` процесс, `awg show wg10 dump` возвращает данные.
