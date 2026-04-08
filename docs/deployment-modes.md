# Cascade — Режимы развёртывания Docker

## Обзор

Cascade поддерживает три режима сетевого развёртывания Docker. Выбор делается один раз при деплое через setup-скрипт.

---

## Режимы

### 1. Host (`network_mode: host`)

- Контейнер разделяет сетевой namespace с хостом
- WireGuard интерфейсы создаются в сетевом namespace хоста
- Проброс портов не нужен — порты сразу доступны снаружи
- Нет изоляции от хоста
- Самый простой в деплое
- **Подходит:** VPS, выделенный сервер

Compose-файл: `docker-compose.go.yml`

### 2. Bridge (`network_mode: bridge` / стандартная сеть Docker)

- Контейнер в отдельном network namespace
- WireGuard интерфейсы создаются внутри namespace контейнера
- **Порты задаются диапазоном** при деплое (например `51820-51829/udp`)
- Docker пробрасывает весь диапазон через iptables DNAT
- Новые интерфейсы создаются в рамках заданного диапазона (`portPool`)
- Расширение диапазона требует перезапуска контейнера
- **Подходит:** обычный Docker хост, нет требований к L2 изоляции

Compose-файл: `deploy/docker-compose.bridge.yml` (генерируется `setup.sh` из `deploy/docker-compose.bridge.yml.example`)

### 3. Isolated / OVS (`network_mode: none`)

- Контейнер стартует только с loopback
- Реальный сетевой интерфейс подключается через OVS (`ovs-docker`) после старта
- Контейнер получает настоящий IP в сети (не Docker-приватный)
- WireGuard интерфейсы создаются внутри namespace контейнера
- Проброс портов не нужен — контейнер имеет реальный IP
- Нет двойного NAT
- Поддержка 802.1q VLAN trunking
- `entrypoint.sh` ждёт появления default route перед запуском Cascade
- `attach.sh` подключает OVS порт, задаёт IP/GW/VLAN/MAC
- **Требует:** Open vSwitch на хосте
- **Подходит:** домашняя лаборатория с VLAN trunk, когда нужен реальный IP для контейнера

Compose-файл: `docker-compose.isolated.yml`

---

## Сравнительная таблица

|                            | Host                  | Bridge                    | Isolated (OVS)              |
|----------------------------|-----------------------|---------------------------|-----------------------------|
| Сложность деплоя           | низкая                | низкая                    | высокая                     |
| Требует OVS                | нет                   | нет                       | да                          |
| Двойной NAT                | нет                   | да                        | нет                         |
| WG интерфейсы              | в хостовом netns      | в контейнерном netns      | в контейнерном netns        |
| Проброс портов             | не нужен              | диапазон при деплое       | не нужен                    |
| Динамические интерфейсы    | без ограничений       | в рамках диапазона        | без ограничений             |
| VLAN поддержка             | нет                   | нет                       | да                          |
| Изоляция от хоста          | нет                   | да                        | да                          |

---

## Setup-скрипт (планируемый)

При деплое пользователь выбирает режим. Скрипт:

1. Спрашивает режим: `host` / `bridge` / `isolated`
2. **Host**: запускает `docker-compose.go.yml`
3. **Bridge**: спрашивает диапазон портов → генерирует compose → запускает
4. **Isolated**: спрашивает OVS bridge, IP, gateway, VLAN → запускает контейнер → выполняет `attach.sh`

---

## Текущее состояние реализации

| Режим     | Compose файл                                                          | Статус                                        |
|-----------|-----------------------------------------------------------------------|-----------------------------------------------|
| Host      | `docker-compose.go.yml`                                               | готов, в production                           |
| Bridge    | `deploy/docker-compose.bridge.yml.example` → генерируется setup.sh   | ✅ реализован                                  |
| Isolated  | `docker-compose.isolated.yml` + `deploy/ovs/attach.sh`               | ✅ реализован, протестирован                   |

---

## Технические детали — Isolated режим

### entrypoint.sh

Необходим для isolated режима. Должен быть в master и в GHCR образе (обратно совместим с host режимом через `WAIT_FOR_NETWORK=0` по умолчанию).

Функции:

- Устанавливает `net.ipv4.ip_forward=1` и `src_valid_mark=1`
- При `WAIT_FOR_NETWORK=1`: ждёт появления default route до запуска Cascade
- Запускает `cascade`

Переменные окружения:

| Переменная              | По умолчанию | Описание                                                    |
|-------------------------|--------------|-------------------------------------------------------------|
| `WAIT_FOR_NETWORK`      | `0`          | `1` — ждать default route перед стартом (isolated режим)   |
| `NETWORK_WAIT_TIMEOUT`  | `60`         | Секунды ожидания до принудительного старта                  |

### attach.sh

Запускается на хосте после старта контейнера. Расположен в `deploy/ovs/attach.sh`.

Действия:

1. Читает параметры из `deploy/.env` (или интерактивно при первом запуске)
2. Запускает контейнер если не запущен
3. `ovs-docker add-port` — подключает veth к OVS bridge
4. Назначает IP и gateway
5. Устанавливает детерминированный MAC (производится из IP: `02:00:<октеты>`)
6. Устанавливает VLAN tag через `ovs-vsctl`
7. Добавляет default route в контейнер

Параметры `deploy/.env` для isolated режима:

| Переменная          | Пример                  | Описание                                            |
|---------------------|-------------------------|-----------------------------------------------------|
| `OVS_BRIDGE`        | `br-trunk`              | Имя OVS bridge на хосте                             |
| `OVS_IP`            | `192.168.20.5/24`       | IP-адрес контейнера с маской                        |
| `OVS_GATEWAY`       | `192.168.20.1`          | Default gateway                                     |
| `OVS_VLAN`          | `20`                    | VLAN ID (опционально, для 802.1q)                   |
| `OVS_IFACE`         | `eth0`                  | Имя интерфейса внутри контейнера (по умолчанию)     |
| `OVS_MAC`           | `02:00:c0:a8:14:05`     | Фиксированный MAC (по умолчанию — деривируется из IP) |
| `CASCADE_CONTAINER` | `cascade`               | Имя контейнера (по умолчанию)                       |

### Определение публичного IP (endpoint для клиентов)

`WG_HOST` не сохраняется в `.env` из `attach.sh`. Cascade автоопределяет endpoint через внешние сервисы (`ip.sb`, `ifconfig.me` и др.).

**Проблема в isolated режиме:** DNS не настроен в `network_mode: none` → внешние сервисы недоступны → fallback на `ip route get 8.8.8.8` → возвращает приватный OVS IP.

**Решение:** добавить DNS в `docker-compose.isolated.yml`:

```yaml
dns:
  - 8.8.8.8
  - 8.8.4.4
```

### Автоматический re-attach при перезапуске контейнера

При `restart: unless-stopped` контейнер стартует с `network_mode: none` и ждёт OVS. Для автоматического подключения создаётся systemd unit:

```ini
[Unit]
Description=Attach OVS port to Cascade container
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/bash /path/to/cascade/deploy/ovs/attach.sh
ExecStop=/usr/bin/bash /path/to/cascade/deploy/ovs/detach.sh

[Install]
WantedBy=multi-user.target
```

---

## Открытые вопросы

1. **`entrypoint.sh` в master**: нужно перенести в master чтобы GHCR образ поддерживал isolated режим без пересборки
2. **DNS в isolated режиме**: добавить `dns:` в `docker-compose.isolated.yml`
3. **Setup-скрипт**: ✅ реализован — `deploy/setup.sh` спрашивает режим (Step 2b), генерирует compose-файл для bridge, записывает PortPool в SQLite
