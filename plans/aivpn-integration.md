# aivpn Integration — Wishlist

## Суть

aivpn (https://github.com/infosave2007/aivpn) — Rust VPN с маскировкой трафика
под реальные приложения (Zoom, TikTok, WebRTC, QUIC, DNS).
Написан автором, PR с HTTP management API принят в upstream.

Локальный клон: `/Users/jenya/PycharmProjects/aivpn/aivpn`

---

## Архитектура интеграции

### Контейнеры

```
docker-compose
├── cascade          (Go, --network host)
│   └── /run/aivpn/ ─── shared volume ───┐
│                                         │
└── cascade-aivpn   (Rust, --network host)│
    ├── /run/aivpn/api.sock ◄─────────────┘
    ├── cap: NET_ADMIN
    └── /dev/net/tun
```

### Host namespace — ключевое наблюдение

Оба контейнера работают в host network namespace.
aivpn создаёт TUN интерфейс (`tun0` или `aivpn0`) в том же namespace.
Cascade видит его через `ip link show` как обычный сетевой интерфейс.

**Никакого специального routing-кода не нужно** — существующие
Gateways / FirewallRules / NAT / Routing уже умеют работать с любым интерфейсом.

### Типичный сценарий использования

```
WG клиент
    │
    ▼
Cascade PBR (rule: dst = заблокированные ресурсы)
    │
    ├── обычный трафик ──► eth0 ──► интернет
    └── заблокированный ──► tun0 (aivpn, выглядит как Zoom) ──► интернет
```

---

## Что нужно реализовать

### Часть 1: Routing (из коробки, ничего делать не надо)
- tun0 появляется в списке интерфейсов автоматически
- Gateway → IP внутреннего адреса aivpn туннеля
- Firewall Rule → PBR через tun0
- NAT → MASQUERADE через tun0

### Часть 2: Client management UI (~3-4 дня)
- Новая страница в сайдбаре — "AIVPN"
- Статус aivpn-server (online/offline)
- Список клиентов
- Add client → имя → получить connection key (`aivpn://...`) + QR
- Delete client
- Cascade ↔ aivpn-server через Unix socket (`/run/aivpn/api.sock`)

### Часть 3: docker-compose (~0.5 дня)
- Добавить `cascade-aivpn` сервис в docker-compose.yml
- Shared volume для Unix socket
- Настройки: NET_ADMIN, /dev/net/tun, --network host

---

## Важные детали

- **aivpn-server как клиент чужого сервера** — если Cascade сам подключается
  к внешнему aivpn серверу (клиентский режим): запустить aivpn-client в контейнере,
  tun0 появится автоматически, routing через Cascade.
- **Производительность**: UDP + Rust userspace, аналогично AWG2 userspace.
  Overhead маскировки добавляет CPU (нейронная обработка масок).
- **Маски трафика**: WebRTC, QUIC, DNS — встроенные; можно записывать свои.

---

## Статус

⏳ Wishlist — не запланировано к реализации.
HTTP management API добавлен в upstream aivpn, PR принят.
