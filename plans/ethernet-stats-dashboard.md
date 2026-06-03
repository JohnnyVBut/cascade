# Plan: Ethernet Interface Stats in Dashboard (Interfaces → All)

**Date:** 2026-03-26
**Branch:** feature/go-rewrite
**Complexity:** Medium overall (2 small + 3 medium steps)

---

## Задача

Показывать физические Ethernet-интерфейсы хоста (eth0, ens3, etc.) на вкладке
Interfaces → All (dashboard) — только статистику трафика (RX/TX bytes/s) и
опционально графики ApexCharts. Без кнопок управления WireGuard-пирами, без QR, без
enable/disable.

---

## 1. Как сейчас работает dashboard

### Структура шаблона (index.html, строки 756–971)

```
activePage === 'interfaces'
  ├── Tab bar: (+) | All | wg10 | wg11 | ...
  ├── v-if="!activeInterfaceId"  ← DASHBOARD
  │     ├── v-if="tunnelInterfaces.length === 0" → "No interfaces yet"
  │     └── v-else
  │           ├── v-if="allPeers.length === 0" → "No peers across all interfaces"
  │           └── v-else → table: v-for="peer in allPeers"
  │                 ├── ApexChart (tx/rx overlay)
  │                 ├── Avatar + Name + badges (interfaceName, S2S)
  │                 ├── Address, AllowedIPs, speed, last seen, endpoint
  │                 ├── Traffic stats panel (uiTrafficStats mode)
  │                 └── Buttons: enable/disable, QR, download, delete
  └── v-if="currentInterface" ← PER-INTERFACE VIEW
```

### Polling (app.js, строки 2935–2948)

`setInterval` каждую секунду вызывает:
- `refreshAllPeers({ updateCharts })` в dashboard-режиме

### `refreshAllPeers()` (app.js, строки 2282–2340)

Итерирует `this.tunnelInterfaces`, для каждого делает `api.getTunnelInterfacePeers()`,
собирает `allPeers` — плоский массив. Логика дельта-вычисления RX/TX хранится в
`peersPersist[peer.id]`.

### Данные для карточки пира

Поля в `allPeers[i]`:
- `id`, `name`, `interfaceId`, `interfaceName`
- `transferRx`, `transferTx` — live kernel счётчики (нарастающие)
- `totalRx`, `totalTx` — персистентный итог (из migration v11)
- `transferRxCurrent`, `transferTxCurrent` — скорость (bytes/s, вычисляется клиентом)
- `transferRxSeries`, `transferTxSeries` — данные для ApexCharts
- `latestHandshakeAt`, `runtimeEndpoint`, `enabled`, `peerType`, `downloadableConfig`

---

## 2. Что есть в backend для интерфейсов хоста

### `nat.GetNetworkInterfaces()` (internal/nat/manager.go, строки 117–147)

Запускает `ip -o link show`, парсит строки, возвращает `[]HostInterface{Name string}`.
**Только имена, без счётчиков трафика.**

### `GET /api/nat/interfaces` (internal/api/nat.go, строки 29–38)

Возвращает `{ interfaces: [{name: "eth0"}, ...] }`. Используется только NAT-дропдауном.
Нет поллинга с фронтенда — вызывается один раз при переходе на страницу NAT.

### Существующего endpoint для статистики интерфейсов нет

Поиск по всему Go-коду подтвердил: ни `/proc/net/dev`, ни `/sys/class/net`, ни
`rx_bytes`/`tx_bytes` нигде не парсятся. Нет никакого `HostInterface` с полем трафика.

---

## 3. Источник данных для Ethernet статистики

### `/proc/net/dev` — рекомендуемый источник

Формат (Linux):
```
Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  123456      100    0    0    0     0          0         0  123456      100    0    0    0     0       0          0
  eth0: 987654321  54321    0    0    0     0          0         0  123456789  43210    0    0    0     0       0          0
```

Поля (0-based после имени интерфейса):
- col 0: rx_bytes, col 1: rx_packets, col 3: rx_drop
- col 8: tx_bytes, col 9: tx_packets, col 11: tx_drop

Доступен ТОЛЬКО на Linux — вполне нормально, т.к. `util.Exec` уже возвращает `"", nil`
на macOS. Но `/proc/net/dev` читается через `os.ReadFile`, а не через `exec.Command`,
поэтому нужна отдельная обработка non-Linux.

### Альтернатива: `ip -s link show eth0`

Подходит, но: (а) требует запуска subprocess, (б) нарушает FIX-11 дух (текстовый
парсинг нестандартного формата), (в) медленнее чем `/proc/net/dev` при polling 1s.
**Рекомендация: `/proc/net/dev`** — это простой текстовый файл, читаемый без subprocess.

### Частота чтения

1 раз в секунду — абсолютно нормально. `/proc/net/dev` — виртуальный файл ядра,
никакого IO на диск, чтение занимает <1ms.

### Какие интерфейсы показывать

Не все: нужно исключить:
- `lo` (loopback)
- `wg*`, `awg*` (WireGuard-интерфейсы — они уже показаны отдельно через peers)
- `docker*`, `br-*`, `veth*` (контейнерные виртуальные интерфейсы)

**Фильтр:** показывать только интерфейсы, которые есть в `ip -o link show` И
у которых имя не начинается с `wg`, `awg`, `lo`, `docker`, `br-`, `veth`.
Альтернативно — whitelist через тип: только физические (`ether` тип в `ip link`).
Простейший подход: фильтр по префиксу имени (достаточно для типичного сервера).

---

## 4. Архитектурные варианты

### Вариант A: Отдельный endpoint `GET /api/host/interfaces`

```json
{
  "interfaces": [
    {
      "name": "eth0",
      "rxBytes": 987654321,
      "txBytes": 123456789,
      "rxPackets": 54321,
      "txPackets": 43210
    }
  ]
}
```

**Плюсы:**
- Чистое разделение ответственности
- Не трогает NAT endpoint (нет регрессий)
- Можно добавить фильтрацию по типу интерфейса
- Можно легко расширить (errors, drops)

**Минусы:**
- Новый package/файл

**Рекомендация: Вариант A.** Чистее, изолированнее.

### Вариант B: Расширить `/api/nat/interfaces`

Добавить `rxBytes`, `txBytes` в существующий `HostInterface`.

**Плюсы:** меньше кода.

**Минусы:**
- NAT endpoint не предназначен для трафик-статистики
- Frontend NAT-дропдаун будет получать лишние данные
- Контракт `/api/nat/interfaces` → `{interfaces: [{name}]}` уже используется
- Риск регрессий в NAT UI

**Отклонено.**

---

## 5. Вопрос о персистировании накопленного трафика

### Проблема

`/proc/net/dev` счётчики **не сбрасываются при перезапуске контейнера** (контейнер
использует `--network host` → интерфейсы принадлежат хосту, а не неймспейсу контейнера).
Счётчики сбрасываются только при:
- Перезагрузке физического сервера (ifdown/ifup или reboot)
- Смене физического интерфейса

### Вывод

**Персистировать не нужно.** Счётчики из `/proc/net/dev` уже достаточно стабильны
(переживают перезапуск контейнера). Достаточно хранить только предыдущее значение
в памяти для вычисления delta (bytes/s). Это аналогично тому, как `peersPersist`
хранится в `app.js` только в памяти браузера.

Если сервер перезагружается — счётчики сбрасываются на хосте, и это нормально:
они показывают трафик с момента последней загрузки ядра. Показывать это явно
в UI ("since boot: X bytes") было бы корректно.

---

## 6. Детальный план реализации

### Шаг 1 — Backend: новый пакет `internal/hostnet` (Medium)

**Новый файл:** `/Users/jenya/PycharmProjects/cascade/internal/hostnet/stats.go`

Содержимое:
- Struct `InterfaceStats { Name, RxBytes, TxBytes, RxPackets, TxPackets int64 }`
- Функция `ReadStats() ([]InterfaceStats, error)` — читает `/proc/net/dev`,
  парсит текстовый формат, фильтрует интерфейсы
- Фильтр: пропускать `lo`, а также интерфейсы с префиксами `wg`, `awg`, `docker`,
  `br-`, `veth`, `tun`, `tap`
- На non-Linux: возвращать пустой slice без ошибки (аналогично `util.Exec`)

Парсинг `/proc/net/dev`:
```
skip header lines 1-2
for each line:
    trim, split on ":"
    name = trim(parts[0])
    fields = strings.Fields(parts[1])
    rxBytes   = parseI64(fields[0])
    rxPackets = parseI64(fields[1])
    txBytes   = parseI64(fields[8])
    txPackets = parseI64(fields[9])
```

**Нет subprocess, нет exec, нет -j флагов.** Только `os.ReadFile("/proc/net/dev")`.

**Новый тест:** `/Users/jenya/PycharmProjects/cascade/internal/hostnet/stats_test.go`
- Тест с mock-данными `/proc/net/dev` в строке → проверить парсинг
- Тест фильтрации (wg0, lo → не включаются, eth0 → включается)

---

### Шаг 2 — Backend: API handler (Small)

**Новый файл:** `/Users/jenya/PycharmProjects/cascade/internal/api/host.go`

```go
// GET /api/host/interfaces
// Returns stats for physical host interfaces (excluding loopback and WG tunnels).
// Polled by the frontend dashboard every second.
func getHostInterfaces(c *fiber.Ctx) error {
    stats, err := hostnet.ReadStats()
    if err != nil || stats == nil {
        stats = []hostnet.InterfaceStats{}
    }
    return c.JSON(fiber.Map{"interfaces": stats})
}
```

**Регистрация в `main.go`:**
```go
api.RegisterHost(apiGroup)
```

Функция `RegisterHost(api fiber.Router)` добавляется в `api/host.go`:
```go
func RegisterHost(api fiber.Router) {
    api.Get("/host/interfaces", getHostInterfaces)
}
```

**Важно:** endpoint за `AuthMiddleware` — уже требует авторизации автоматически,
т.к. регистрируется в `apiGroup` после `apiGroup.Use(api.AuthMiddleware)`.

---

### Шаг 3 — Frontend: API-метод (Small)

**Файл:** `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/js/api.js`

Добавить метод `getHostInterfaces()`:
```javascript
async getHostInterfaces() {
  return this.call({ method: 'GET', path: '/host/interfaces' });
}
```

---

### Шаг 4 — Frontend: data + polling (Medium)

**Файл:** `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/js/app.js`

**4a. Новые поля в `data`:**
```javascript
hostInterfaces: [],         // [{name, rxBytes, txBytes, ...}] — текущие значения из API
hostIfacePersist: {},       // keyed by name: {rxPrevious, txPrevious, rxCurrent, txCurrent, rxHistory, txHistory, rxSeries, txSeries}
```

**4b. Новый метод `refreshHostInterfaces({ updateCharts = false } = {})`:**

Логика идентична паттерну `refreshAllPeers` / `refreshPeers`:
1. `const res = await this.api.getHostInterfaces()`
2. Для каждого интерфейса из `res.interfaces`:
   - Если нет в `hostIfacePersist[name]` — инициализировать как первый тик
     (history=Array(50).fill(0), rxPrevious=rxBytes, txPrevious=txBytes)
   - Иначе: `rxCurrent = rxBytes - rxPrevious`, `txCurrent = txBytes - txPrevious`
     (max(0, ...) чтобы не уходить в минус при перезагрузке сервера)
   - Обновить `rxPrevious = rxBytes`, `txPrevious = txBytes`
   - Если updateCharts: push в history, shift, обновить Series
3. `this.hostInterfaces = res.interfaces.map(iface => ({ ...iface, persist: hostIfacePersist[iface.name] }))`

**4c. Расширить polling в `setInterval`:**
```javascript
if (this.activePage === 'interfaces' && !this.activeInterfaceId) {
    // уже есть: this.refreshAllPeers(...)
    this.refreshHostInterfaces({ updateCharts: this.updateCharts }).catch(console.error);
}
```

**4d. Расширить `watch: { activeInterfaceId }`:**
```javascript
// при переключении на dashboard (newId = null):
this.refreshHostInterfaces({ updateCharts: false });
```

**4e. Расширить `mounted()` / login handler:**
```javascript
this.loadTunnelInterfaces().then(() => {
    if (!this.activeInterfaceId) {
        this.refreshAllPeers();
        this.refreshHostInterfaces();   // ← добавить
    }
});
```

---

### Шаг 5 — Frontend: UI карточки (Medium)

**Файл:** `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/index.html`

**Место вставки:** внутри `v-if="!activeInterfaceId"`, ПЕРЕД блоком `allPeers`.
Точка вставки — после закрывающего `</div>` блока "No interfaces" и перед
`v-else` блока `allPeers`. Структурно: добавить новую секцию ВЫШЕ списка пиров,
или НИЖЕ. Рекомендация — ВЫШЕ (хост-уровень важнее, чем конкретные пиры).

**Структура секции:**
```html
<!-- Host Ethernet Interfaces -->
<div v-if="hostInterfaces.length > 0" class="shadow-md rounded-lg bg-white dark:bg-neutral-700 overflow-hidden mb-4">
  <!-- Заголовок секции -->
  <div style="padding:12px 20px; border-bottom:1px solid #e5e7eb;" class="dark:border-neutral-600">
    <span class="text-sm font-medium text-gray-500 dark:text-neutral-400 uppercase tracking-wide">
      Host Interfaces
    </span>
  </div>
  <!-- Карточки интерфейсов -->
  <div v-for="iface in hostInterfaces" :key="iface.name"
    class="relative overflow-hidden border-b last:border-b-0 border-gray-100 dark:border-neutral-600 border-solid">

    <!-- ApexCharts (аналогично peer-картам, уже существующие chartOptionsTX/RX) -->
    <div v-if="uiChartType && uiShowCharts && iface.persist"
      :class="`absolute z-0 bottom-0 left-0 right-0 h-6 ${uiChartType === 1 && 'line-chart'}`">
      <apexchart width="100%" height="100%" :options="chartOptionsTX" :series="iface.persist.txSeries || []"></apexchart>
    </div>
    <div v-if="uiChartType && uiShowCharts && iface.persist"
      :class="`absolute z-0 top-0 left-0 right-0 h-6 ${uiChartType === 1 && 'line-chart'}`">
      <apexchart width="100%" height="100%" :options="chartOptionsRX" :series="iface.persist.rxSeries || []" style="transform:scaleY(-1);"></apexchart>
    </div>

    <div class="relative py-3 md:py-5 px-3 z-10 flex flex-col sm:flex-row justify-between gap-3">
      <div class="flex gap-3 md:gap-4 items-center">
        <!-- Иконка: Ethernet (заменяет аватар пира) -->
        <div class="h-10 w-10 mt-2 self-start rounded-full bg-gray-50 dark:bg-neutral-600 flex items-center justify-center">
          <!-- SVG: network/ethernet иконка -->
          <svg class="w-5 h-5 text-gray-400" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
              d="M5 12h14M12 5l7 7-7 7"/>
          </svg>
        </div>

        <!-- Имя + метки -->
        <div class="flex flex-col flex-grow gap-1">
          <div class="text-gray-700 dark:text-neutral-200 text-sm md:text-base font-medium">
            {{ iface.name }}
            <span style="margin-left:6px;font-size:0.65rem;padding:1px 6px;background:rgba(107,114,128,0.12);color:#6b7280;border-radius:4px;font-weight:600;vertical-align:middle;display:inline-block;">
              HOST
            </span>
          </div>
          <!-- Скорость (inline режим) -->
          <div v-if="!uiTrafficStats && iface.persist" class="text-gray-500 dark:text-neutral-400 text-xs">
            <span v-if="iface.persist.txCurrent" class="whitespace-nowrap">
              <!-- TX arrow down -->
              ↓ {{iface.persist.txCurrent | bytes}}/s
            </span>
            <span v-if="iface.persist.rxCurrent" class="whitespace-nowrap ml-2">
              <!-- RX arrow up -->
              ↑ {{iface.persist.rxCurrent | bytes}}/s
            </span>
            <span class="text-gray-400 dark:text-neutral-500 ml-2">
              since boot: {{bytes(iface.rxBytes)}} rx / {{bytes(iface.txBytes)}} tx
            </span>
          </div>
        </div>
      </div>

      <!-- Traffic stats panel (uiTrafficStats mode) -->
      <div v-if="uiTrafficStats && iface.persist"
        class="flex gap-2 items-center shrink-0 text-gray-400 dark:text-neutral-400 text-xs mt-px justify-end">
        <div class="min-w-20 md:min-w-24" v-if="iface.txBytes">
          <span class="flex gap-1">
            <!-- TX arrow -->
            <svg class="align-middle h-3 inline mt-0.5" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor">
              <path fill-rule="evenodd" d="M16.707 10.293a1 1 0 010 1.414l-6 6a1 1 0 01-1.414 0l-6-6a1 1 0 111.414-1.414L9 14.586V3a1 1 0 012 0v11.586l4.293-4.293a1 1 0 011.414 0z" clip-rule="evenodd" />
            </svg>
            <div>
              <span class="text-gray-700 dark:text-neutral-200">{{iface.persist.txCurrent | bytes}}/s</span>
              <br><span style="font-size:0.85em">{{bytes(iface.txBytes)}}</span>
            </div>
          </span>
        </div>
        <div class="min-w-20 md:min-w-24" v-if="iface.rxBytes">
          <span class="flex gap-1">
            <!-- RX arrow -->
            <svg class="align-middle h-3 inline mt-0.5" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor">
              <path fill-rule="evenodd" d="M3.293 9.707a1 1 0 010-1.414l6-6a1 1 0 011.414 0l6 6a1 1 0 01-1.414 1.414L11 5.414V17a1 1 0 11-2 0V5.414L4.707 9.707a1 1 0 01-1.414 0z" clip-rule="evenodd" />
            </svg>
            <div>
              <span class="text-gray-700 dark:text-neutral-200">{{iface.persist.rxCurrent | bytes}}/s</span>
              <br><span style="font-size:0.85em">{{bytes(iface.rxBytes)}}</span>
            </div>
          </span>
        </div>
      </div>
      <!-- НАМЕРЕННО: нет кнопок enable/disable, QR, download, delete -->
    </div>
  </div>
</div>
```

**CSS:** все используемые классы (`flex`, `gap-2`, `text-sm`, `rounded-lg` и т.д.)
уже существуют в `src/www/css/app.css` или в vendor Tailwind CDN. SVG-иконки и
inline styles не требуют новых классов. Следует проверить по ПРАВИЛУ №2.

---

## 7. Файлы для изменения

| Файл | Тип изменения | Сложность |
|------|---------------|-----------|
| `internal/hostnet/stats.go` (новый) | Новый пакет: парсинг `/proc/net/dev`, фильтрация интерфейсов | Small |
| `internal/hostnet/stats_test.go` (новый) | Unit-тесты парсинга с mock-данными | Small |
| `internal/api/host.go` (новый) | Handler + RegisterHost() | Small |
| `cmd/awg-easy/main.go` | Добавить `api.RegisterHost(apiGroup)` (одна строка) | Small |
| `internal/frontend/www/js/api.js` | Добавить метод `getHostInterfaces()` | Small |
| `internal/frontend/www/js/app.js` | Добавить `hostInterfaces`, `hostIfacePersist`, `refreshHostInterfaces()`, расширить polling и watchers | Medium |
| `internal/frontend/www/index.html` | Добавить секцию "Host Interfaces" с карточками в dashboard | Medium |

**Итого:** 7 файлов (2 новых пакета, 1 новый handler, 1 строка в main.go, 3 frontend-файла).

---

## 8. Файлы, которые НЕ нужно трогать

| Файл | Причина |
|------|---------|
| `internal/nat/manager.go` | `GetNetworkInterfaces()` остаётся без изменений |
| `internal/api/nat.go` | `/api/nat/interfaces` не меняется |
| `internal/db/db.go` | Никаких новых таблиц — трафик Ethernet не персистируется |
| Любые существующие миграции | Не трогать никогда |
| `internal/peer/peer.go` | Не связано |
| `internal/tunnel/*.go` | Не связано |
| `internal/frontend/www/css/app.css` | Прекомпилированный файл (ПРАВИЛО №2) |

---

## 9. Риски и ограничения

### Риск 1: `/proc/net/dev` недоступен в некоторых контейнерных окружениях

**Симптом:** `/proc/net/dev` существует, но показывает только `lo` (часть
network namespace конфигураций).

**Для данного проекта:** контейнер запускается с `--network host` → виден полный
список интерфейсов хоста. Риск минимален.

**Смягчение:** если `os.ReadFile("/proc/net/dev")` возвращает ошибку, handler
возвращает `{ interfaces: [] }` без ошибки. Frontend просто не показывает секцию.

### Риск 2: WireGuard-интерфейсы попадают в список

`wg10`, `wg11` существуют в `/proc/net/dev`. Фильтр по префиксу `wg` их исключит.
Аналогично `awg*`. Если пользователь создал интерфейс с нестандартным именем
(не через это приложение) — может попасть в список. Это приемлемо: это его физический
или виртуальный интерфейс хоста.

**Смягчение:** дополнительно можно фильтровать по списку ID интерфейсов из
`tunnel.GetAll()` — но это создаёт cross-package зависимость между `hostnet` и `tunnel`.
Проще оставить фильтрацию по именам: `wg*`, `awg*` — достаточно.

### Риск 3: Переполнение int64 счётчиков `/proc/net/dev`

Ядро Linux использует `unsigned long` для счётчиков (64-bit на 64-bit системах).
При очень высоком трафике (>9.2 EB) возможно переполнение. Практически недостижимо.

### Риск 4: Скорость "прыгает" при первом рендере

Первый тик `refreshHostInterfaces` инициализирует baseline (delta = 0). На втором
тике появляется реальная скорость. Аналогично поведению peer-карточек — это норма.

### Риск 5: Tailwind-классы в новых карточках

ПРАВИЛО №2: проверять каждый используемый класс в `internal/frontend/www/css/app.css`.
Судя по существующему коду dashboard (строки 801–970), классы `flex`, `gap-2`,
`gap-3`, `py-3`, `md:py-5`, `px-3`, `z-10`, `relative`, `overflow-hidden`, `border-b`,
`last:border-b-0`, `border-solid`, `shrink-0`, `mt-px`, `justify-end`, `min-w-20`,
`md:min-w-24`, `text-gray-700`, `dark:text-neutral-200`, `text-gray-500`,
`dark:text-neutral-400`, `text-xs`, `whitespace-nowrap` — **все уже используются
в dashboard-секции** и заведомо присутствуют в CSS.

Специфичные для новых карточек классы: `uppercase`, `tracking-wide` — нужно проверить.
Если отсутствуют → заменить на `style="text-transform:uppercase; letter-spacing:0.05em;"`.

`items-center` — уже используется в строке 814 dashboard. Присутствует.
`font-medium` — строка 831. Присутствует.

**Действие перед имплементацией:** для каждого нового класса выполнить:
```
grep "имя-класса" internal/frontend/www/css/app.css
```

### Риск 6: Backward compatibility — старые фронтенды

Новый endpoint `GET /api/host/interfaces` — аддитивное изменение. Если frontend
закеширован и не вызывает этот endpoint — ничего не ломается. `hostInterfaces`
инициализируется пустым массивом, секция `v-if="hostInterfaces.length > 0"` не рендерится.

### Риск 7: Сервер без физических интерфейсов (VPS только с WireGuard)

Если все нефильтруемые интерфейсы — только WireGuard (wg*, awg*), секция
`hostInterfaces` будет пустой → `v-if="hostInterfaces.length > 0"` скроет секцию.
Поведение корректное.

---

## 10. Оценка трудоёмкости

| Шаг | Описание | Сложность | Время |
|-----|----------|-----------|-------|
| 1 | `hostnet/stats.go` + тест | Small | 1.5 ч |
| 2 | `api/host.go` + регистрация | Small | 0.5 ч |
| 3 | `api.js` метод | Small | 0.2 ч |
| 4 | `app.js` data + polling | Medium | 1.5 ч |
| 5 | `index.html` UI карточки | Medium | 2 ч |
| — | Проверка CSS-классов (ПРАВИЛО №2) | Small | 0.5 ч |
| **Итого** | | | **~6 часов** |

---

## 11. Рекомендация

**Стоит делать. Вариант A (отдельный endpoint).**

Обоснование:
1. Ethernet-интерфейсы дают пользователю полную картину сетевой активности сервера
   (не только WG-трафик, но и весь входящий/исходящий).
2. Реализация изолирована: новый пакет `hostnet` не затрагивает существующий код.
3. `/proc/net/dev` — надёжный, быстрый, без subprocess.
4. Frontend-изменения используют уже отработанные паттерны (`peersPersist` →
   `hostIfacePersist`, `chartOptionsTX/RX` переиспользуются).
5. Нет персистирования в DB — счётчики хоста стабильны между рестартами контейнера.
6. Секция появляется только если есть не-WG интерфейсы (`v-if`), не ломает
   существующий UX при отсутствии физических интерфейсов.

---

## 12. Порядок коммитов

```
1. feat(hostnet): parse /proc/net/dev — host interface traffic stats
2. feat(api): GET /api/host/interfaces — host ethernet stats endpoint
3. feat(ui): dashboard — host interface traffic cards with charts
```
(Можно один коммит, если объём небольшой.)

---

## Абсолютные пути всех затрагиваемых файлов

**Новые файлы:**
- `/Users/jenya/PycharmProjects/cascade/internal/hostnet/stats.go`
- `/Users/jenya/PycharmProjects/cascade/internal/hostnet/stats_test.go`
- `/Users/jenya/PycharmProjects/cascade/internal/api/host.go`

**Изменяемые файлы:**
- `/Users/jenya/PycharmProjects/cascade/cmd/awg-easy/main.go`
- `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/js/api.js`
- `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/js/app.js`
- `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/index.html`

**Файлы, которые НЕ трогать:**
- `/Users/jenya/PycharmProjects/cascade/internal/nat/manager.go`
- `/Users/jenya/PycharmProjects/cascade/internal/api/nat.go`
- `/Users/jenya/PycharmProjects/cascade/internal/db/db.go`
- `/Users/jenya/PycharmProjects/cascade/internal/frontend/www/css/app.css`
