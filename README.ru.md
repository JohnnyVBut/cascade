<p align="center">
  <img src="./assets/logo.svg" width="240" alt="Cascade" />
</p>

<h1 align="center">Cascade</h1>

<p align="center">
  <strong>Платформа управления роутером WireGuard / AmneziaWG с веб-интерфейсом</strong>
</p>

<p align="center">
  <a href="https://github.com/JohnnyVBut/cascade/actions/workflows/docker-publish.yml">
    <img src="https://github.com/JohnnyVBut/cascade/actions/workflows/docker-publish.yml/badge.svg" alt="Build" />
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/JohnnyVBut/cascade" alt="License" />
  </a>
  <img src="https://img.shields.io/badge/Go-1.23-blue" alt="Go 1.23" />
  <img src="https://img.shields.io/badge/AmneziaWG-3.0-purple" alt="AmneziaWG 3.0" />
</p>

<p align="center">
  <a href="README.md">🇬🇧 English</a>
  &nbsp;·&nbsp;
  <a href="docs/USER_MANUAL.md">📖 Руководство пользователя</a>
</p>

---

<img width="1484" height="775" alt="image" src="https://github.com/user-attachments/assets/01be9f90-afc5-452c-ad5e-25bfa586ba2b" />

## Содержание

- [Возможности](#-возможности)
- [Требования](#-требования)
- [Я новичок — установить Cascade](#-я-новичок--установить-cascade)
- [У меня уже есть Cascade — обновить](#-у-меня-уже-есть-cascade--обновить)
- [Переключить режим AWG на работающей установке](#-переключить-режим-awg-на-работающей-установке)
- [TLS: тестовый и настоящий сертификат](#-tls-тестовый-и-настоящий-сертификат)
- [Справочник конфигурации](#️-справочник-конфигурации)
- [Модель безопасности](#-модель-безопасности)
- [Совместимые VPN-клиенты](#-совместимые-vpn-клиенты)
- [Диагностика проблем](#️-диагностика-проблем)
- [REST API](#-rest-api)
- [Документация](#-документация)
- [Стек технологий](#️-стек-технологий)
- [Поддержать проект](#-поддержать-проект)

---

## ✨ Возможности

| Модуль | Описание |
|--------|----------|
| 🔌 **Интерфейсы** | Несколько туннельных интерфейсов WireGuard / AmneziaWG (2.0 и **3.0**), быстрое создание, импорт `.conf` как аплинк, MSS clamping на интерфейс |
| 👥 **Пиры** | Клиентские и site-to-site (S2S) пиры с QR-кодами, статистика трафика за всё время, ограничение полосы на клиента и членство в группах |
| 🌐 **Маршрутизация** | Статические маршруты, policy-based routing (PBR), просмотр маршрутов ядра, OSPF в планах |
| 🔀 **NAT** | Исходящий MASQUERADE / SNAT с поддержкой алиасов + Port Forwarding (DNAT) с областью действия на интерфейс |
| 🛡️ **Firewall** | Правила фильтрации (ACCEPT / DROP / REJECT) + PBR через gateway |
| 📋 **Алиасы** | 7 типов: host, network, ipset, client-group, group, port, port-group. Client-группы на базе ipset, автообновление при изменении пиров |
| 📡 **Gateways** | Живой мониторинг ping + HTTP, группы gateway, автоматический failover |
| 🎛️ **AWG-шаблоны** | Шаблоны параметров обфускации AmneziaWG 2.0 и **3.0** со встроенным генератором, включая поля Transport Protection (S3/S4) для AWG 3.0 |
| 🔐 **Авторизация** | Мультипользовательские аккаунты, TOTP 2FA (Google Authenticator), долгоживущие API-токены |
| 🔒 **TLS** | Let's Encrypt через acme.sh (короткоживущий сертификат для голого IP или обычный для домена) |
| 🎭 **Decoy-сайт** | Caddy-прокси показывает фейковый стриминговый сайт на `/`; админка спрятана за секретным путём |
| 🖥️ **Мульти-сервер** | Управление несколькими роутерами Cascade из одного интерфейса — переключение серверов в сайдбаре, прозрачное проксирование всех API-запросов, поддержка self-signed сертификатов |
| 📊 **Мониторинг** | Метрики трафика в реальном времени на интерфейс, история статуса gateway (stacked bar chart), страница Diagnostics с историей за период |
| ⚡ **Speed Test** | Speed test на базе iperf3 между любыми управляемыми серверами — режимы Auto / Tunnel / Internet, автоопределение S2S-туннеля, история результатов |
| 🚦 **Ограничение скорости** | Ограничение полосы по client-группам через tc HTB (kbps down/up на IP) |
| 🧙 **Мастера** | Пошаговые мастера настройки: простой клиентский VPN, Cascade через WireGuard-аплинк, интерконнект Cascade ↔ Cascade S2S |
| 💾 **Бэкап** | `deploy/backup.sh` — бэкап данных одной командой перед апгрейдом |

### Почему Cascade?

- ✅ **Go-бинарник** — единый статический бинарник, без Node.js, без npm, без зависимостей
- ✅ **Мульти-интерфейс** — управление несколькими интерфейсами WireGuard/AWG из одного UI
- ✅ **Полный AmneziaWG 2.0 и 3.0** — S3, S4, I5, Transport Protection, H-range обфускация, 7 CPS-профилей + browser fingerprint
- ✅ **Policy-based routing** — маршрутизация трафика по источнику через разные gateway
- ✅ **Port Forwarding (DNAT)** — прозрачное каскадирование трафика с опциональным source NAT
- ✅ **Импорт .conf как аплинк** — подключение Cascade как клиента к любому WireGuard-серверу; использование как PBR gateway без ручного вмешательства в таблицу маршрутизации
- ✅ **Мониторинг gateway** — ICMP ping + HTTP/S проверки, автоматический fallback при сбое
- ✅ **Мультипользовательский режим + TOTP 2FA** — отдельные аккаунты с поддержкой Google Authenticator
- ✅ **HTTPS по умолчанию** — Caddy + acme.sh, работает даже с голыми IP через короткоживущие сертификаты Let's Encrypt
- ✅ **Decoy-защита** — путь админки скрыт; посетители видят фейковый стриминговый сайт
- ✅ **Управление несколькими серверами** — контроль нескольких роутеров Cascade из одной вкладки браузера с прозрачным проксированием API
- ✅ **Встроенный speed test** — iperf3 между управляемыми серверами, автоопределение S2S-туннеля, история результатов
- ✅ **Мониторинг трафика** — метрики на интерфейс и история состояния gateway с настраиваемым периодом
- ✅ **Мастера настройки** — пошаговые мастера для Uplink VPN и S2S-интерконнекта; автосоздание интерфейсов, алиасов, gateway, PBR-правил и NAT за один проход

---

## 📋 Требования

- Ubuntu 22.04 или 24.04 (другие дистрибутивы — ручная настройка)
- Root-доступ
- Публичный IP-адрес или доменное имя
- Порты: `443/tcp` (HTTPS), `51820+/udp` (WireGuard)
- `git` (на минимальных/урезанных образах VPS его часто нет — поставь заранее, если `git clone` ниже упадёт с "command not found"):
  ```bash
  apt-get update && apt-get install -y git
  ```

---

## 🆕 Я новичок — установить Cascade

Перед запуском нужно определиться с двумя вещами:

1. **Вариант развёртывания** — почти всем нужен **Full stack** (HTTPS, decoy-сайт, всё уже настроено и связано).
   **Router only** — только если у тебя уже есть свой reverse proxy / firewall / доступ только через VPN.
2. **Режим работы AWG** — **Userspace** рекомендован по умолчанию: работает на любом VPS, без ребута, без deadlock'ов kernel-модуля.
   **Kernel module** — только если нужна максимальная пропускная способность и ты готов мириться с
   [известными deadlock-проблемами](https://github.com/amnezia-vpn/amneziawg-linux-kernel-module/issues/146).

### Самый быстрый путь (Full stack + Userspace)

Одна команда на свежем VPS, от root:

```bash
curl -fsSL https://raw.githubusercontent.com/JohnnyVBut/cascade/master/deploy/quickstart-userspace.sh | sudo bash
```

Всё. В конце получишь URL админки вида `https://YOUR_IP/<secret-path>/` — открой его,
создай первого пользователя (авторизация не требуется, пока нет ни одного аккаунта) и включи
**TOTP 2FA** в Settings → Users.

> Если `curl` виснет или таймаутится на `raw.githubusercontent.com` — некоторые сети/провайдеры
> не могут достучаться до Fastly-CDN GitHub'а (`185.199.108-111.133`), хотя `github.com` сам
> доступен. Тогда склонируй репозиторий и запусти скрипт локально:
> ```bash
> apt-get update && apt-get install -y git
> git clone https://github.com/JohnnyVBut/cascade.git
> cd cascade
> sudo bash deploy/quickstart-userspace.sh
> ```

Нужен kernel-режим? Та же идея, другой скрипт:

```bash
curl -fsSL https://raw.githubusercontent.com/JohnnyVBut/cascade/master/deploy/quickstart-kernel.sh | sudo bash
```

Тестируешь и не хочешь тратить лимит production-сертификатов Let's Encrypt? Добавь `--staging`
к любому из скриптов — см. [TLS: тестовый и настоящий сертификат](#-tls-тестовый-и-настоящий-сертификат).

<details>
<summary><strong>Хочешь пройти шаги вручную, или нужен Router-only / Bridge-режим сети?</strong></summary>

#### Full stack по шагам

```bash
git clone https://github.com/JohnnyVBut/cascade.git
cd cascade
sudo bash deploy/setup.sh          # интерактивно — спросит режим работы, режим сети, IP, путь админки, email
# или: sudo bash deploy/setup.sh --yes   (всё по умолчанию: userspace, host-сеть, автоопределённый IP)
```

| Шаг | Что происходит |
|-----|-----------------|
| 0 | 1 ГБ swap (защита от OOM при сборке) |
| 1 | Апгрейд ядра до HWE 6.x (только Ubuntu 22.04) — ребут, затем повторный запуск |
| 2 | **Режим работы AmneziaWG** — Userspace (рекомендуется) или Kernel module |
| 2b | **Режим сети Docker** — Host (по умолчанию) или Bridge (диапазон портов для публикации Docker) |
| 3 | Установка Docker CE |
| 4 | sysctl: `ip_forward`, UDP-буферы |
| 4b | TCP-тюнинг: BBR congestion control, FQ scheduler, `rp_filter` |
| 5a | Генерация decoy-видео через ffmpeg (60 сек шума — выглядит как настоящий стрим) |
| 5 | Сборка Docker-образа Cascade |
| 6 | Интерактивный сбор конфигурации (IP, секретный путь, email) |
| 7 | Запуск Cascade (только localhost) |
| 8 | Выпуск TLS-сертификата через acme.sh (Let's Encrypt) |
| 9 | Запуск Caddy (HTTPS + decoy-сайт + скрытый путь админки) |
| 10 | Проверка: health-check Cascade + Caddy, вывод итога |

`setup.sh` идемпотентен — безопасно перезапускать после ребута или обновления.

#### Только роутер (продвинутый вариант)

Только контейнер Cascade, слушает **исключительно localhost** — без публичного доступа, без TLS.
Ответственность за сетевую безопасность, авторизацию и контроль доступа — на тебе.

```bash
git clone https://github.com/JohnnyVBut/cascade.git
cd cascade
docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml up -d
# UI доступен на http://127.0.0.1:8888/
```

Пошаговое руководство: [docs/DEPLOY.md](docs/DEPLOY.md)

</details>

---

## 🔄 У меня уже есть Cascade — обновить

Какую команду выполнять, зависит только от **режима работы AWG** — если не уверен, посмотри
бейдж в сайдбаре веб-интерфейса (синий = userspace, зелёный = kernel). Что означает каждый режим
— см. [Режимы работы AWG](#️-режимы-работы-awg).

### Userspace-режим

Безопасно, особый порядок не нужен — CLI и реализация протокола живут в одном образе и
обновляются вместе атомарно:

```bash
cd cascade
git pull origin master
docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml up -d
```

Full stack (с Caddy):
```bash
cd cascade
git pull origin master
sudo bash deploy/setup.sh --yes
```

### Kernel module режим

⚠️ **Здесь важен порядок.** Docker-образ следует протокольной ветке AmneziaWG `:latest`, но
kernel-модуль на хосте сам не обновляется — если они разъедутся, интерфейсы не запустятся с
ошибкой `Unable to modify interface: Invalid argument`. В первую очередь это касается установок
**до перехода на AmneziaWG 3.0 (2026-07-30)**.

Скачай новый образ, но **не** запускай `up -d` сразу — дай `switch-mode.sh --kernel` пересинхронизировать
модуль и перезапустить контейнер одним шагом, чтобы CLI и модуль переключились синхронно:

```bash
cd cascade
git pull origin master
docker compose -f docker-compose.yml pull
sudo bash deploy/switch-mode.sh --kernel
```

Начиная с v0.9.5, `--kernel` каждый раз перепроверяет версию пакета `ppa:amnezia/ppa` — даже
если модуль уже загружен — и перезагружает его, если доступна более новая сборка. Это реальная
пересинхронизация, а не no-op.

---

## 🔁 Переключить режим AWG на работающей установке

```bash
sudo bash deploy/switch-mode.sh --userspace   # → amneziawg-go (стабильно)
sudo bash deploy/switch-mode.sh --kernel      # → kernel module (быстро)
```

Скрипт сам делает установку/выгрузку kernel-модуля, blacklist и перезапуск контейнера.

> **Это не то же самое, что quickstart-скрипты.** `quickstart-kernel.sh` / `quickstart-userspace.sh`
> предназначены только для **новой** установки — повторный запуск на уже настроенной системе
> **не** переключит режим, потому что `setup.sh` подгружает существующий `deploy/.env`, и он
> перекрывает режим, который пытался задать quickstart-скрипт. Для смены режима используй
> `switch-mode.sh`.

---

## 🔒 TLS: тестовый и настоящий сертификат

Добавь `--staging` к `setup.sh` или к любому из quickstart-скриптов, чтобы выпустить недоверенный
сертификат от [тестового CA Let's Encrypt](https://letsencrypt.org/docs/staging-environment/)
вместо настоящего:

```bash
sudo bash deploy/setup.sh --staging        # тестовый CA (браузер покажет предупреждение — это ожидаемо)
sudo bash deploy/setup.sh --yes --staging  # неинтерактивно + staging
```

**Когда использовать staging:**
- Повторные установки/переустановки на одном домене во время тестирования — в
  [лимиты production Let's Encrypt](https://letsencrypt.org/docs/rate-limits/) (5 сертификатов
  на набор доменов в неделю) легко упереться при итерациях, а у staging их фактически нет
- CI, одноразовый VPS или тестирование установочных скриптов (например, quickstart-скриптов)
- Любой прогон, где нужно просто убедиться, что установка проходит и TLS настроен, без
  необходимости в доверенном браузером сертификате

**Переход staging → production:** убери флаг staging и перезапусти. `setup.sh` сам определяет
издателя текущего сертификата и меняет его — вручную удалять ничего не нужно:

```bash
sed -i '/^ACME_STAGING=/d' deploy/.env    # или вручную удали эту строку
sudo bash deploy/setup.sh --yes
```

`setup.sh` увидит `CERT_MODE=staging` при `ACME_STAGING=0`, удалит staging-сертификат и выпустит
настоящий от production CA. (Обратное — повторный запуск с `--staging`, когда уже установлен
production-сертификат — **не** перезапишет его, чтобы случайно не выбросить рабочий сертификат.)

---

## ⚙️ Режимы работы AWG

| | Userspace (`amneziawg-go`) | Kernel module |
|---|---|---|
| Производительность | ~70% от kernel | Максимальная |
| Стабильность | ✅ Стабильно | ⚠️ Известные deadlock'и |
| Нужен kernel-модуль | ❌ Нет | ✅ Да |
| Работает на любом VPS | ✅ Да | Зависит от ядра |
| Ребут после установки | ❌ Нет | Иногда |

Текущий режим отображается бейджем в сайдбаре веб-интерфейса (синий = userspace, зелёный = kernel).
Режим сети Docker показан отдельным бейджем (серый = HOST, янтарный = BRIDGE, красный = NONE).

---

## ⚙️ Справочник конфигурации

Конфигурация собирается интерактивно `setup.sh` и сохраняется в `deploy/.env`.

| Переменная | По умолчанию | Описание |
|-----------|--------------|----------|
| `WG_HOST` | автоопределение | Публичный IP или домен сервера |
| `ADMIN_PATH` | случайный hex | Секретный путь для админки (например, `/a1b2c3d4.../`) |
| `PORT` | `8888` | Внутренний порт Cascade (Caddy проксирует на него) |
| `BIND_ADDR` | `127.0.0.1` | Адрес привязки Cascade (используй `127.0.0.1` за Caddy) |
| `ACME_EMAIL` | опционально | Email для уведомлений Let's Encrypt |
| `ACME_STAGING` | `0` | `1` = использовать тестовый CA (недоверенный сертификат, без лимитов — для тестов) |
| `AWG_USERSPACE_IMPL` | `amneziawg-go` | `amneziawg-go` или `kernel` |
| `NETWORK_MODE` | `host` | `host` или `bridge` — режим сети Docker |
| `BRIDGE_PORT_RANGE` | *(только для bridge)* | Публикуемый диапазон UDP-портов для WireGuard в bridge-режиме (например, `51831-65535`) |

Остальные настройки (значения WireGuard по умолчанию, DNS и т.д.) настраиваются в веб-интерфейсе, в разделе **Settings**.

---

## 🔒 Модель безопасности

- Админка отдаётся только по `https://HOST/<ADMIN_PATH>/` — обычный `https://HOST/` показывает decoy-сайт
- HTTPS с HTTP/3 (QUIC) через Caddy
- TLS-сертификаты: короткоживущие (6 дней) для голых IP, обычные 90-дневные для доменов
- Сессионная cookie: `HttpOnly`, `Secure`, `SameSite=Strict`
- Хэширование паролей bcrypt (cost 12)
- **Мультипользовательские аккаунты** — у каждого пользователя свой логин и пароль
- **TOTP 2FA** — Google Authenticator / Authy (включается на пользователя в Settings → Users)
- **API-токены** — долгоживущие bearer-токены для скриптов; обходят TOTP; отзываемые
- Валидация входных данных на всех эндпоинтах API

Полная модель угроз: [docs/SECURITY.md](docs/SECURITY.md)

---

## 📱 Совместимые VPN-клиенты

> ⚠️ **Стандартные клиенты WireGuard НЕ работают с интерфейсами AmneziaWG.**
> Интерфейсы WireGuard 1.0 работают со стандартными клиентами как обычно.

| Платформа | Приложение |
|-----------|------------|
| Android | [Amnezia VPN](https://play.google.com/store/apps/details?id=org.amnezia.vpn) · [AmneziaWG](https://play.google.com/store/apps/details?id=org.amnezia.awg) |
| iOS / macOS | [Amnezia VPN](https://apps.apple.com/app/amneziavpn/id1600529900) · [AmneziaWG](https://apps.apple.com/app/amneziawg/id6478942365) |
| Windows | [Amnezia VPN](https://github.com/amnezia-vpn/amnezia-client/releases) · [AmneziaWG](https://github.com/amnezia-vpn/amneziawg-windows-client/releases) |
| Linux | [amneziawg-tools](https://github.com/amnezia-vpn/amneziawg-tools) · [Amnezia VPN](https://github.com/amnezia-vpn/amnezia-client/releases) |

---

## 🛠️ Диагностика проблем

**Проверить статус контейнера:**
```bash
docker logs cascade
docker compose -f deploy/caddy/docker-compose.yml logs
```

**Проверить интерфейсы WireGuard:**
```bash
docker exec cascade awg show
docker exec cascade wg show
```

**Проверить режим работы AWG:**
```bash
docker exec cascade env | grep WG_QUICK
# WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go  → userspace
# (пусто или отсутствует)                          → kernel module
```

**Проверить firewall / NAT:**
```bash
docker exec cascade iptables-nft -t nat -L -n -v
docker exec cascade ip rule show
```

**Переключить режим AWG:**
```bash
sudo bash deploy/switch-mode.sh --userspace
sudo bash deploy/switch-mode.sh --kernel
```

**Перезапустить setup (например, после ребута или обновления сертификата):**
```bash
sudo bash deploy/setup.sh
```

**Сделать бэкап перед рискованной операцией:**
```bash
sudo bash deploy/backup.sh
```

---

## 🔌 REST API

Cascade предоставляет полноценный REST API — всё, что делает веб-интерфейс, можно автоматизировать скриптами.

```bash
# Авторизация
curl -c cookies.txt -X POST http://127.0.0.1:8888/api/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"yourpassword"}'

# Список интерфейсов
curl -b cookies.txt http://127.0.0.1:8888/api/tunnel-interfaces

# Создать пира
curl -b cookies.txt -X POST http://127.0.0.1:8888/api/tunnel-interfaces/wg10/peers \
  -H "Content-Type: application/json" \
  -d '{"name":"laptop"}'
```

Используй для автоматизации выдачи пиров, интеграции со своими дашбордами или создания кастомных клиентов.

Полный справочник: [docs/API.md (RU)](docs/API.md) · [docs/API.en.md (EN)](docs/API.en.md)

---

## 📖 Документация

- [Руководство по развёртыванию](docs/DEPLOY.md)
- [Справочник API (RU)](docs/API.md)
- [Справочник API (EN)](docs/API.en.md)
- [Модель безопасности](docs/SECURITY.md)

---

## 🏗️ Стек технологий

| Слой | Технология |
|------|-----------|
| Backend | Go 1.23, Fiber v2 |
| Frontend | Vue 2, Tailwind CSS (встроен в бинарник) |
| База данных | SQLite (`modernc.org/sqlite`, без CGO) |
| Reverse proxy | Caddy 2 (HTTP/3 + QUIC) |
| VPN | AmneziaWG 2.0 / 3.0, WireGuard 1.0 |

---

## ☕ Поддержать проект

Если Cascade оказался полезен — можно поддержать разработку:

| Способ | Адрес |
|--------|-------|
| TRC20  | `TDm1VvwoLaRdjpp7149QNacBzQtXnGresW` |
| Юмани RU | https://yoomoney.ru/to/4100119568549598 |

---

## 🙏 Благодарности

- Основано на [wg-easy](https://github.com/wg-easy/wg-easy)
- [AmneziaVPN](https://github.com/amnezia-vpn) за протокол AmneziaWG
- [Vadim-Khristenko/AmneziaWG-Architect](https://github.com/Vadim-Khristenko/AmneziaWG-Architect) — математика и код для генерации профилей обфускации AWG 2.0 (CPS-сигнатуры, H-диапазоны, подгонка размера пакетов под browser fingerprint)

## 📄 Лицензия

MIT — см. [LICENSE](LICENSE)

---

<p align="center">Сделано с ❤️ для безопасного и приватного доступа в интернет</p>
