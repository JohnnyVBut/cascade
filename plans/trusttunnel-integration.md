# TrustTunnel Integration — TODO / Plan

Статус: исследование и ручной прототип (2026-09). В код Cascade ничего не внесено.

## Зачем

ТСПУ блокирует S2S AWG-туннель РФ ↔ зарубеж: исходящие соединения РФ-сервера к
заблокированным зарубежным IP (TCP и UDP) режутся. Нужен транспорт, который
устанавливает соединение **из-за границы в РФ** и не ловится DPI.

TrustTunnel (https://github.com/TrustTunnel/TrustTunnel, клиент
https://github.com/TrustTunnel/TrustTunnelClient, Apache-2.0) — VPN-протокол
поверх HTTP/1.1, HTTP/2, HTTP/3. Endpoint на Rust, клиент на C++ с TUN-режимом.

## Что проверено по исходникам

- **Сервер не имеет TUN.** Это L4-прокси: на каждый клиентский поток открывает
  обычный `TcpStream::connect` / UDP-сокет. Нет клиентского IP из подсети,
  поэтому firewall/PBR/`tc` Cascade к трафику клиентов TT неприменимы.
  `SO_BINDTODEVICE` используется только для ICMP (`icmp_forwarder.rs`).
- **Направление:** соединение всегда открывает клиент, трафик выходит на endpoint.
  Для «выход за границей, соединение из-за границы» endpoint должен стоять в РФ,
  а клиент за границей — поэтому нужен **транспорт-носитель для AWG**, а не
  прямой выход через TT.
- **Клиент имеет TUN-режим** (`[listener.tun]`). Ключевые параметры:
  `device_name`, `use_existing = true` (клиент цепляется к уже созданному
  устройству), `included_routes = []` (клиент не трогает маршруты; по умолчанию
  ставит таблицу 880 и `ip rule` 30800/30801 и заворачивает **весь** трафик),
  `change_system_dns = false`, `killswitch_enabled = false`.
- **Маршрутизация endpoint по SNI** (`tls_demultiplexer.rs`): неизвестный SNI
  (в том числе пустой, то есть заход браузером на голый IP) → `Unexpected SNI`,
  обрыв. Внутри хоста по пути: ping, speedtest, `reverse_proxy.path_mask`,
  остальное — туннель. У клиента есть `custom_sni`.
- **`SIGHUP` перезагружает только `hosts.toml`.** Смена `credentials.toml` /
  `rules.toml` требует перезапуска endpoint (рвёт активные сессии).
- Метрики: `[metrics]`, `/metrics` (Prometheus), `/clients` (JSON; только при
  `per_client_metrics = true`, содержит имена и IP — держать на 127.0.0.1).
- Пароль пользователей хранится открытым текстом в `credentials.toml`.

## Целевая схема

```
РФ-сервер (Cascade)                          Зарубежный сервер (Cascade)
  TT endpoint :443  ◄═ TCP (HTTP/2) ═══════  TT client, tt0 (use_existing)
  AWG-интерфейс (peer без Endpoint) ◄─ UDP внутри TT ─ AWG (Endpoint = RU_IP:AWG_PORT,
                                                            keepalive 25)
```

- На зарубежной стороне PBR-правило: только UDP на `RU_IP:AWG_PORT` идёт в `tt0`
  (`ip rule add to RU_IP ipproto udp dport AWG_PORT lookup 4242`; таблица 4242,
  `default dev tt0`), плюс `rp_filter` на `tt0` = 0.
- MTU AWG-интерфейсов снизить до 1230–1280 (внешний пакет должен влезать в 1350 `tt0`).
- Endpoint на РФ-стороне обращается к собственному публичному IP (глобальный
  адрес проходит проверку `is_global_ip`; при NAT нужен
  `allow_private_network_connections = true`).
- Дальше всё как сейчас: gateway на AWG-интерфейсе, gateway groups, PBR.

## Результаты ручного прототипа (на 2026-09-26)

- Endpoint на РФ-стороне и клиент за границей развёрнуты, клиент подключался.
- **Порт 8443 к РФ-серверу не проходил** (SYN-ACK с исходного порта 8443
  терялся). Порт 443 проходил (страница Caddy). После смены IP зарубежного
  сервера поведение изменилось — какие порты проходят с нового IP, не выяснено.
- Не завершено: TCP- и UDP-тесты через `tt0`, подключение AWG через туннель.
- Подводные камни, на которые уже наступили:
  - без `use_existing` / `included_routes = []` клиент забирает весь трафик сервера;
  - `setup_wizard` сам пишет `device_name = ""`, `use_existing = false`,
    `change_system_dns = true` — при правке конфига убирать дубликаты ключей;
  - `tt0` не переживает перезагрузку/пересоздание сети — создавать при старте;
  - `pkill` сравнивает имя по 15 символам, использовать `pkill -f`;
  - Caddy занимает 443 (см. ниже).

## Открытый вопрос: TrustTunnel + Caddy на 443

Наш Caddy — стандартный `caddy:alpine` (без плагинов; сборку через xcaddy убрали
из-за RAM на маленьких VPS). Он не умеет SNI-passthrough и не проксирует
CONNECT-туннели, поэтому «просто проксировать TT через Caddy» нельзя.

| Вариант | Суть | Минусы |
|---|---|---|
| A. TT на 443, Caddy за ним | `[reverse_proxy] server_address=127.0.0.1:8080`, `path_mask=/<ADMIN_PATH>` | Нет SNI при заходе на голый IP → админка и декой по IP недоступны (нужен домен или SSH-туннель) |
| B. SNI-маршрутизатор (nginx `stream` + `ssl_preread` или HAProxy) | SNI-служебное имя → TT, остальное (в т.ч. пустой SNI) → Caddy | Лишний компонент |
| C. Caddy с `caddy-l4` | То же, что B, внутри Caddy | Нужен кастомный образ; собирать в GitHub Actions, публиковать в GHCR |

Предварительная рекомендация: для проверки B, для продукта C. Клиенту задать
`custom_sni`; сертификат TT можно взять тот же, что у Caddy (acme.sh, LE на IP).
**Проверить:** против чего клиент сверяет имя при `custom_sni` (IP из `hostname`
или сам SNI).

## TODO

- [ ] Выяснить, какие порты проходят с нового IP зарубежного сервера; определить,
      блокируется ли 8443 и нужен ли вообще вариант с 443.
- [ ] Прогнать TCP/UDP-тесты через `tt0` (см. «Ручной прототип»).
- [ ] Поднять AWG через туннель (Endpoint на зарубежном peer, keepalive 25,
      пустой Endpoint на РФ-peer, MTU 1230–1280) и измерить скорость Speed Test-ом
      (`http2`, при необходимости `http3`).
- [ ] Проверить: `gateway` без next-hop на `tt0` (`GatewayIP` обязателен, host-route
      через `ip route replace <ip>/32 dev <dev>` в `internal/gateway/manager.go`).
- [ ] Проверить поведение килсвитча клиента на Linux.
- [ ] Выбрать вариант A/B/C для совместной работы с Caddy.
- [ ] Проверить `post_quantum_group_enabled=false` / `anti_dpi` / `tls_profile`, если
      DPI режет ClientHello (симптом: зависший `curl` на TLS).
- [ ] Реализация в Cascade: `internal/trusttunnel` (роли endpoint и client),
      миграция, API, страница UI, PBR для порта AWG, создание `tt0` при старте,
      автосопряжение через Remotes, gateway `tt0` и включение в gateway group.
- [ ] Документация в тех же коммитах: ARCHITECTURE, API.en/API.md, SECURITY
      (пароли в открытом виде, `/clients` только на localhost), FEATURES.

## Ручной прототип (кратко)

РФ-сервер: установка `/opt/trusttunnel` (`install.sh`), `setup_wizard` (порт
listen, пользователь, самоподписанный сертификат), `systemctl enable --now trusttunnel`,
экспорт клиентского конфига `trusttunnel_endpoint vpn.toml hosts.toml -c <user> -a <RU_IP:port> --format toml`.

Зарубежный сервер: установка `/opt/trusttunnel_client`, `setup_wizard --mode non-interactive
--endpoint_config ...`, в `trusttunnel_client.toml`: `killswitch_enabled=false`,
в `[listener.tun]`: `device_name="tt0"`, `use_existing=true`, `included_routes=[]`,
`change_system_dns=false` (удалить дублирующиеся строки от мастера). Создать
`tt0` (`ip tuntap add dev tt0 mode tun`, адрес `10.255.77.2/30`, MTU 1350), `rp_filter`,
`ip route`/`ip rule` для таблицы 4242, запустить клиента. Проверки: TCP
(`curl --interface 10.255.77.2 http://RU_IP:PORT/`) и UDP (`nc -u -s 10.255.77.2`).

## Сопоставление с Xray (справка)

Xray имеет встроенный reverse proxy (bridge/portal), который мог бы решить
направление без вложенного AWG; не проверялось, интеграция тяжелее (JSON-конфиги,
tun2socks). Решили продолжать с TrustTunnel.
