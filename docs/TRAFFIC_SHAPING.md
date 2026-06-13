# Traffic Shaping (Per-Client Bandwidth Limiting)

## Overview

Cascade ограничивает скорость отдельных VPN-клиентов через Linux Traffic Control (`tc`).
Реализация находится в `internal/tc/tc.go`.

Ограничения применяются на уровне WireGuard-интерфейса и работают независимо для каждого направления:

| Направление | Термин | Механизм |
|-------------|--------|-----------|
| Сервер → Клиент | Download ↓ (egress) | HTB qdisc + class + u32 filter |
| Клиент → Сервер | Upload ↑ (ingress) | ingress qdisc + police filter |

---

## Egress (Download ↓)

```
WireGuard interface (wg0)
│
└── root qdisc: HTB handle 1:
    ├── class 1:3e7  (default, rate=1gbit)   ← все без ограничений
    ├── class 1:5    (peer 10.8.0.5, rate=X)
    │   └── filter: ip dst 10.8.0.5/32 → flowid 1:5
    └── class 1:105  (peer 10.8.1.5, rate=Y)
        └── filter: ip dst 10.8.1.5/32 → flowid 1:105
```

**Как работает:**

1. При старте интерфейса `EnsureQdisc()` создаёт root HTB qdisc с default class `1:3e7` (999 hex) на `1gbit`.
2. Для каждого ограниченного пира `Apply()` создаёт HTB class и u32 filter, матчащий `ip dst <peer_ip>/32`.
3. Пакеты до пира направляются в его class и шейпятся по заданной скорости.
4. Пакеты остальных клиентов попадают в default class и не ограничиваются.

**HTB параметры:**

```
rate  = ceil = <target_rate>   # гарантированная = потолок (без заимствования)
burst = rate_kbps * 1000 / 80  # ~100 мс накопленного трафика, минимум 1500 байт
```

`burst` позволяет кратковременно превышать rate при заполненном буфере — важно для TCP slow start.

---

## Ingress (Upload ↑)

```
WireGuard interface (wg0)
│
└── ingress qdisc (handle ffff:)
    ├── filter prio=5:   ip src 10.8.0.5/32  → police rate=X drop
    └── filter prio=261: ip src 10.8.1.5/32  → police rate=Y drop
```

**Как работает:**

Ingress ограничение через HTB невозможно напрямую (ядро не поддерживает HTB на ingress).
Вместо этого используется **police filter** — простой токен-бакет на входящем трафике:

- Пакеты от ограниченного клиента матчатся по `ip src <peer_ip>/32`.
- Если они превышают `rate`, пакеты **дропаются** немедленно (action `drop`).
- TCP реагирует на потери снижением окна — клиент замедляется.

Отличие от HTB: police не буферизует, он дропает избыток. Поэтому upload-ограничение более «жёсткое» и может давать ретрансмиты при приближении к лимиту.

---

## Class ID из IP-адреса

Каждому пиру присваивается `classid` детерминированно из его VPN IP:

```
classid = octet[2] * 256 + octet[3]

10.8.0.5  → classid = 0 * 256 + 5   = 5    (hex: 0x5)
10.8.1.5  → classid = 1 * 256 + 5   = 261  (hex: 0x105)
10.8.3.17 → classid = 3 * 256 + 17  = 785  (hex: 0x311)
```

Резервированные значения:
- `0` — невалидный, пир пропускается
- `999` (0x3e7) — зарезервирован под default class → ремаппится в `1000`

Это позволяет атомарно обновлять правила (`tc class change`) без поиска по базе — classid всегда вычисляем из IP.

---

## Компенсация WireGuard Overhead

`tc` считает **внешние** (зашифрованные, UDP) пакеты, а пользователь ожидает ограничение по **внутреннему** (полезному) трафику.

WireGuard добавляет ~80 байт overhead на пакет:

```
outerMTU = 1500  (стандартный Ethernet)
wgMTU    = 1420  (WireGuard inner MTU по умолчанию)

tcRate = targetKbps * outerMTU / wgMTU
       = targetKbps * 1500 / 1420
       ≈ targetKbps * 1.056
```

Пример: пользователь задаёт `10000 kbps` → tc получает `10563 kbps` → на выходе клиент видит ровно `10 Mbit/s`.

Если интерфейс имеет нестандартный MTU (задан в настройках), используется его значение вместо 1420.

---

## Жизненный цикл правил

```
Interface Start()
    └── tc.EnsureQdisc(ifaceID)      ← создать root HTB + ingress qdisc
    └── tc.RestoreAll(ifaceID, ...)  ← восстановить правила всех ограниченных пиров

Peer rate limit set/update
    └── tc.Apply(ifaceID, peerIP, rateDown, rateUp, mtu)

Peer rate limit removed / peer deleted
    └── tc.Remove(ifaceID, peerIP)   ← Apply(... 0, 0, 0)

Interface Stop() / wg-quick down
    └── правила исчезают автоматически (ядро удаляет qdisc вместе с интерфейсом)
```

Обновление правил атомарно: фильтры удаляются и пересоздаются, HTB class обновляется через `tc class change` (без teardown).

---

## Текущее использование

Сейчас ограничение скорости применяется только в **Expired Peer Policy** (`restrict` режим):

- Пир с истёкшим сроком (`expiredAt`) не отключается, а получает rate limit.
- `expiredPeerRateDown` / `expiredPeerRateUp` — глобальные настройки (kbps), 0 = без ограничения.
- При продлении даты rate limit снимается автоматически (`tc.Remove`).

Per-client rate limit из UI (задать скорость конкретному пиру) — не реализован, инфраструктура готова.

---

## Диагностика

Посмотреть текущие правила на интерфейсе:

```bash
# Egress: HTB классы и фильтры
tc -s class show dev wg0
tc filter show dev wg0

# Ingress: police фильтры
tc -s filter show dev wg0 parent ffff:

# Статистика: счётчики пакетов/байт/дропов
tc -s qdisc show dev wg0
```

Счётчик `dropped` в police filter — количество дропнутых пакетов при превышении upload-лимита. Большое значение означает что клиент систематически упирается в потолок.
