# PoC: Split Routing — RU ISP / KZ Tunnel

**Статус: ✅ ПРОТЕСТИРОВАНО**

## Задача

VPN-клиенты подключены к российскому серверу.
- Российские IP → напрямую через российского провайдера
- Нероссийские IP → через S2S туннель в Казахстан
- KZ сервер видит **реальные IP клиентов** (не туннельный IP)

## Топология

```
VPN Client (192.168.72.x)
        |
    wg11 (RU сервер, 192.168.72.1/24)
        |
    [ipset + fwmark + ip rule]
        |
   ┌────┴────────────────────┐
   │                         │
Russian IP              Non-Russian IP
   │                         │
ens3 → ISP RU          wg10 → KZ сервер (10.255.255.1)
(62.***.***.*)               │
                        MASQUERADE → eth0 → Internet
                        (185.**.*.*)
```

## Серверы

| Роль | Интерфейс | IP |
|------|-----------|----|
| RU VPN клиенты | wg11 | 192.168.72.1/24 |
| RU → KZ туннель | wg10 | 10.255.255.2/30 |
| RU ISP | ens3 | gateway 62.***.***.* |
| KZ туннельный IP | wg10 | 10.255.255.1 |
| KZ ISP | eth0 | gateway 185.**.*.* |

## Реализация

### Российский сервер

```bash
# 1. Загрузить российские префиксы в ipset (~12K записей, агрегированные)
ipset create ru_nets hash:net family inet
(echo "create ru_nets hash:net family inet -exist"; \
 awk '{print "add ru_nets " $1}' /root/scripto/ru_agregate.txt) | ipset restore -!

# 2. Добавить таблицу маршрутизации
echo "100 vpn_kz" >> /etc/iproute2/rt_tables

# 3. Маршрут в таблице 100: всё → KZ
ip route add default via 10.255.255.1 dev wg10 table 100

# 4. Правило: fwmark 1 → таблица 100
ip rule add fwmark 1 lookup 100 priority 100

# 5. Пометить нероссийский трафик от VPN-клиентов
iptables-nft -t mangle -A PREROUTING \
    -s 192.168.72.0/24 \
    -m set ! --match-set ru_nets dst \
    -j MARK --set-mark 1
```

### KZ сервер

```bash
# 1. Маршрут назад к клиентской сети
ip route add 192.168.72.0/24 via 10.255.255.2 dev wg10

# 2. MASQUERADE клиентского трафика
iptables -t nat -A POSTROUTING -s 192.168.72.0/24 -o eth0 -j MASQUERADE
```

## Принцип работы

1. Пакет от клиента (`192.168.72.x`) приходит на wg11
2. PREROUTING mangle: dst в `ru_nets`? → без mark → main table → ISP → MASQUERADE к RU публичному IP
3. PREROUTING mangle: dst НЕ в `ru_nets`? → mark=1
4. Routing: mark=1 → lookup table 100 → default via wg10 → KZ
5. POSTROUTING на RU: трафик в wg10 **не натится** (MASQUERADE ограничен `-o $ISP`)
6. KZ сервер: принимает пакет (AllowedIPs=0.0.0.0/0), видит src=192.168.72.x ✓
7. KZ сервер: MASQUERADE → выходит с KZ публичным IP
8. Ответ: KZ → de-NAT → route 192.168.72.0/24 via wg10 → RU → клиент

## Ключевые решения

- **ipset hash:net** вместо 12K маршрутов в routing table — O(1) lookup в ядре
- **fwmark** вместо src-based ip rule — гибче, готово к расширению
- **NAT на KZ стороне** — KZ видит реальные IP клиентов (192.168.72.x)
- **AllowedIPs=0.0.0.0/0 + disableRoutes=true** на KZ пире — туннель принимает любой src IP, маршруты не добавляются автоматически
- **MASQUERADE с `-o $ISP`** — клиентский интерфейс натит **только в провайдера**, WAN-туннели не затрагиваются

## Подводные камни (обнаружены в процессе)

### 1. Широкий MASQUERADE на клиентском интерфейсе

**Проблема:** PostUp wg11 добавлял `MASQUERADE -s 192.168.72.0/24` без `-o` ограничения.
Трафик клиентов натился к `10.255.255.2` ещё на RU стороне → KZ видел туннельный IP вместо `192.168.72.x`.

**Фикс в коде** (`TunnelInterface.js`, commit `a08aed1`):
```
# Было:
iptables-nft -t nat -A POSTROUTING -s <subnet> -j MASQUERADE

# Стало:
ISP=$(ip -4 route show default | awk 'NR==1{print $5}')
iptables-nft -t nat -A POSTROUTING -s <subnet> -o $ISP -j MASQUERADE
```
ISP интерфейс определяется динамически — работает на любом хосте (eth0, ens3, ens18, ...).

### 2. iptables-legacy vs iptables-nft

На хостах могут сосуществовать оба backend-а. Правила добавленные через `iptables-nft`
не видны в `iptables-legacy` и наоборот. При диагностике проверять оба:
```bash
iptables-nft -t nat -L POSTROUTING -v -n --line-numbers
iptables-legacy -t nat -L POSTROUTING -v -n --line-numbers
```

## Диагностика

```bash
# RU: ipset загружен
ipset list ru_nets | wc -l          # ~12K записей

# RU: правила маршрутизации
ip rule show
ip route show table 100
iptables-nft -t mangle -L PREROUTING -v -n

# RU: POSTROUTING — убедиться что нет широкого MASQUERADE без -o
iptables-nft -t nat -L POSTROUTING -v -n --line-numbers

# RU: проверить куда уйдёт конкретный IP
ip route get 8.8.8.8 mark 1         # → wg10 (KZ)
ip route get 77.88.8.8 mark 0       # Яндекс → ens3 (ISP)

# KZ: маршрут назад и NAT счётчики
ip route show | grep 192.168.72
iptables -t nat -L POSTROUTING -v -n

# KZ: живой трафик — проверить src IP клиентов
tcpdump -i wg10 -n 'src net 192.168.72.0/24' -c 20

# С клиента
traceroute 8.8.8.8      # первый хоп после 192.168.72.1 → KZ
traceroute 77.88.8.8    # прямо через RU ISP
curl ident.me           # должен вернуть KZ публичный IP
```

## Следующие шаги (GUI интеграция)

- Страница **Routing** в AWG-Easy: управление `ip rule` / `ip route` / `ipset` через API
- Загрузка файла префиксов через UI с автообновлением ipset
- Failover: если KZ туннель упал → переключить на ISP или резервный туннель
- Персистентность: восстановление правил после перезагрузки сервера
- Per-client политики: разные маршруты для разных клиентов (fwmark per-client)
