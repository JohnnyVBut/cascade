# Cascade — Bridge Mode, Userspace AWG, без Caddy

Минимальная установка Cascade на хосте **без затрагивания хостовой сети**:

- `network_mode: bridge` — контейнер в отдельном сетевом namespace
- `AWG_USERSPACE_IMPL=amneziawg-go` — без kernel-модуля, без `SYS_MODULE`
- Порты WireGuard UDP проброшены через Docker port mapping
- Web UI доступен напрямую на внешнем IP (без Caddy)
- `iptables`, `ip route`, `ip rule` работают только внутри контейнера

---

## Требования

| | |
|---|---|
| OS | Linux (Ubuntu 22.04+, Debian 11+, любой дистрибутив) |
| Docker | 20.10+ (rootful) или Docker 24+ с rootlesskit |
| Kernel | 5.6+ (для WireGuard в userspace через TUN) |
| RAM | 256 MB минимум |
| `/dev/net/tun` | должен существовать на хосте |

> **Kernel module не нужен.** `amneziawg-go` — чистый userspace, работает без DKMS/PPA.

---

## Шаг 1 — Проверить `/dev/net/tun`

```bash
ls -la /dev/net/tun
# ожидаемо: crw-rw-rw- 1 root root 10, 200 ...
```

Если файла нет:

```bash
modprobe tun
# или постоянно:
echo "tun" >> /etc/modules-load.d/tun.conf
```

Убедиться что TUN работает (создаётся и удаляется интерфейс):

```bash
ip tuntap add dev test-tun mode tun
ip link show test-tun
ip tuntap del dev test-tun mode tun
```

Если `ip tuntap` недоступен — установить `iproute2`:

```bash
apt install -y iproute2
```

---

## Шаг 2 — Установить Docker

```bash
curl -fsSL https://get.docker.com | sh
```

Проверить:

```bash
docker version
# Client: Docker Engine - Community, Version: 24.x ...
```

---

## Шаг 3 — Клонировать репозиторий

```bash
git clone https://github.com/JohnnyVBut/cascade.git
cd cascade
mkdir -p data
```

---

## Шаг 4 — Создать `docker-compose.bridge-userspace.yml`

```yaml
services:
  cascade:
    image: ghcr.io/johnnyvbut/cascade:latest
    container_name: cascade
    restart: unless-stopped

    environment:
      # Публичный IP или домен — попадает в клиентские конфиги WireGuard
      - WG_HOST=YOUR_PUBLIC_IP

      # Порт Web UI внутри контейнера
      - PORT=8888

      # Слушать на всех интерфейсах внутри контейнера
      # (без этого Docker port mapping не работает)
      - BIND_ADDR=0.0.0.0

      # Userspace AWG — без kernel-модуля
      - WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go

      - DEBUG=false

    volumes:
      - ./data:/etc/wireguard/data

    ports:
      # Web UI: только с локального хоста (рекомендуется).
      # Доступ снаружи — через SSH tunnel (см. ниже).
      - "127.0.0.1:8888:8888/tcp"

      # WireGuard UDP: диапазон портов для всех интерфейсов
      # Каждый WireGuard-интерфейс занимает один порт из этого диапазона.
      # Диапазон задаётся при старте контейнера — изменение требует перезапуска.
      # Планируйте с запасом: 10 портов = до 10 WireGuard-интерфейсов.
      - "51820-51829:51820-51829/udp"

    # ip_forward и src_valid_mark — только внутри контейнерного netns,
    # хостовая сеть не затрагивается.
    sysctls:
      - net.ipv4.ip_forward=1
      - net.ipv4.conf.all.src_valid_mark=1

    cap_add:
      - NET_ADMIN
      # SYS_MODULE не нужен в userspace режиме

    devices:
      - /dev/net/tun:/dev/net/tun
```

---

## Шаг 5 — Запустить

```bash
docker compose -f docker-compose.bridge-userspace.yml pull
docker compose -f docker-compose.bridge-userspace.yml up -d
```

Проверить статус:

```bash
docker ps
# CONTAINER ID   IMAGE                              STATUS
# xxxxxxxxxxxx   ghcr.io/johnnyvbut/cascade:latest  Up N seconds

docker logs cascade | tail -20
```

---

## Шаг 6 — Доступ к Web UI

Web UI слушает только на `127.0.0.1:8888` — снаружи напрямую недоступен.

### Вариант А — SSH tunnel (рекомендуется)

Запустить на **локальной машине** (ноутбуке, ПК):

```bash
ssh -L 8888:localhost:8888 user@YOUR_SERVER_IP
```

Пока SSH-сессия открыта — UI доступен в браузере по адресу:
`http://localhost:8888`

Для фонового tunnel без интерактивной сессии:

```bash
ssh -fNL 8888:localhost:8888 user@YOUR_SERVER_IP
```

Закрыть tunnel:
```bash
pkill -f "ssh -fNL 8888"
```

### Вариант Б — прямой доступ по HTTP (только для тестовой среды)

Если сервер в закрытой/домашней сети и HTTP-доступ снаружи приемлем —
изменить в `docker-compose.bridge-userspace.yml`:

```yaml
ports:
  - "0.0.0.0:8888:8888/tcp"   # доступен снаружи напрямую
```

Перезапустить:
```bash
docker compose -f docker-compose.bridge-userspace.yml up -d
```

UI доступен по: `http://YOUR_SERVER_IP:8888`

> ⚠️ **Не использовать в production.** HTTP передаёт сессионные cookie в открытом виде.
> Для production — установить Caddy согласно [DEPLOY.md](DEPLOY.md).

### Проверка API с хоста (в обоих вариантах)

```bash
curl http://127.0.0.1:8888/api/health
# {"host":"...","status":"ok","version":"..."}
```

При первом входе — форма создания администратора. Задайте логин и пароль.

---

## Шаг 7 — Создать WireGuard-интерфейс и пир

1. **Interfaces → + New Interface**
   - Protocol: `AmneziaWG 2.0` (или `WireGuard 1.0`)
   - Address: `10.8.0.1/24`
   - Listen Port: `51820` (из опубликованного диапазона)
   - Нажать **Save**, затем **Start**

2. **+ New Peer** → ввести имя → **Save**

3. На карточке пира нажать **QR Code** или **Download Config**

---

## Шаг 8 — Подключить клиента и проверить

Импортировать конфиг в AmneziaWG (iOS/Android/Desktop) или WireGuard.

После подключения проверить с клиента:

```bash
# Текущий публичный IP должен совпадать с сервером (или его провайдером)
curl ifconfig.io

# Пинг до шлюза WireGuard-интерфейса
ping 10.8.0.1
```

В Web UI на карточке пира должны появиться:
- **Last Handshake** — время (не "Never")
- **RX / TX** — ненулевые значения
- **Endpoint** — IP клиента

---

## Проверка `/dev/net/tun` внутри контейнера

```bash
# Убедиться что устройство пробрасывается в контейнер
docker exec cascade ls -la /dev/net/tun
# crw-rw-rw- 1 root root 10, 200 ...

# Убедиться что amneziawg-go доступен
docker exec cascade which amneziawg-go
# /usr/bin/amneziawg-go

# Проверить что WireGuard-интерфейс создан через TUN (после Start в UI)
docker exec cascade ip -d link show wg10
# wg10: ... tun ...
```

Если интерфейс поднят — `/dev/net/tun` работает корректно. Это означает что
userspace AWG успешно создал TUN-устройство и обрабатывает трафик.

Дополнительная диагностика:

```bash
# Логи контейнера — ошибки amneziawg-go видны здесь
docker logs cascade 2>&1 | grep -i "tun\|wireguard\|error"

# Проверить что AWG слушает порт (внутри контейнера)
docker exec cascade ss -ulnp | grep 51820
# UNCONN  0  0  0.0.0.0:51820  ...
```

---

## Открыть порты в файрволе хоста (если есть)

```bash
# UFW
ufw allow 8888/tcp comment "Cascade Web UI"
ufw allow 51820:51829/udp comment "Cascade WireGuard"

# iptables напрямую
iptables -I INPUT -p tcp --dport 8888 -j ACCEPT
iptables -I INPUT -p udp --dport 51820:51829 -j ACCEPT
```

---

## Обновление

```bash
docker compose -f docker-compose.bridge-userspace.yml pull
docker compose -f docker-compose.bridge-userspace.yml up -d
```

Данные в `./data/` сохраняются — все интерфейсы и пиры восстанавливаются автоматически.

---

## Rootless Docker

Инструкция выше рассчитана на rootful Docker. При использовании **rootless Docker** (Docker 24+ с rootlesskit) необходимы два изменения.

### 1. `/dev/net/tun` — проверить права

В rootless окружении device mount работает только если файл имеет права `0666`:

```bash
ls -la /dev/net/tun
# crw-rw-rw- (0666) — всё хорошо, пробрасывается автоматически
# crw-rw---- (0660) — нужно добавить udev правило:
```

Если права `0660`:

```bash
echo 'KERNEL=="tun", MODE="0666"' > /etc/udev/rules.d/99-tun.rules
udevadm control --reload
udevadm trigger /dev/net/tun
```

### 2. `sysctls` — не работают в rootless, применить на хосте вручную

Rootless Docker не может менять параметры ядра через `sysctls:` в compose-файле.
Удалить блок `sysctls:` из `docker-compose.bridge-userspace.yml` и применить параметры на хосте:

```bash
# Применить сейчас
sysctl -w net.ipv4.ip_forward=1
sysctl -w net.ipv4.conf.all.src_valid_mark=1

# Сохранить постоянно (применится после перезагрузки)
cat >> /etc/sysctl.d/99-cascade.conf << 'EOF'
net.ipv4.ip_forward = 1
net.ipv4.conf.all.src_valid_mark = 1
EOF
```

> `ip_forward` нужен для маршрутизации трафика WireGuard-клиентов.
> `src_valid_mark` нужен для PBR (Policy-Based Routing) — без него fwmark-маршрутизация не работает.

### 3. `CAP_NET_ADMIN` — работает без изменений

В rootless Docker `CAP_NET_ADMIN` предоставляется в пределах user namespace контейнера. Этого достаточно для:

- создания WireGuard TUN-интерфейса через `/dev/net/tun`
- управления `ip route`, `ip rule` внутри контейнерного netns
- `iptables` внутри контейнерного netns

Никаких изменений в compose-файл не требуется — `cap_add: NET_ADMIN` остаётся как есть.

После трёх пунктов выше инструкция применима к rootless Docker без других модификаций.

---

## Отличия от стандартной установки

| | Стандарт (host + Caddy) | Эта инструкция | Rootless |
|--|--|--|--|
| `network_mode` | `host` | `bridge` | `bridge` |
| Сеть хоста | затрагивается | не затрагивается | не затрагивается |
| AWG | kernel module | userspace (`amneziawg-go`) | userspace (`amneziawg-go`) |
| `SYS_MODULE` | нужен | **не нужен** | **не нужен** |
| TLS | Caddy + acme.sh | нет (HTTP) | нет (HTTP) |
| WireGuard порты | прямые (без mapping) | через Docker port mapping | через Docker port mapping |
| `iptables` scope | хост | только контейнер | только контейнер |
| `sysctls` в compose | работают | работают | **не работают — вручную на хосте** |
| `/dev/net/tun` | из коробки | из коробки | нужны права `0666` |
