# AWG-Easy 2.0 — Claude Memory

## ⚠️ ПРАВИЛО №0: ДОКУМЕНТАЦИЯ API — ОБНОВЛЯТЬ ОБА ФАЙЛА

При добавлении, изменении или удалении **любого API endpoint** — обязательно обновить **оба** файла:
- `docs/API.md` — русская версия
- `docs/API.en.md` — английская версия

Оба файла должны быть синхронизированы и попасть в **один коммит** вместе с изменениями в `Server.js`.
Никогда не коммитить новый endpoint без обновления документации.

---

## ⚠️ ПРАВИЛО №1: ПЕРЕД РЕДАКТИРОВАНИЕМ ЛЮБОГО ФАЙЛА

**ВСЕГДА читать файл целиком через Read tool, ТОЛЬКО ПОТОМ делать точечное изменение через Edit.**
Никогда не писать код "из головы" или "по памяти" — только читать → редактировать.
Это предотвращает случайное уничтожение уже исправленных багов.

## ⚠️ ПРАВИЛО №2: TAILWIND CSS — ТОЛЬКО СУЩЕСТВУЮЩИЕ КЛАССЫ

`src/www/css/app.css` — прекомпилированный статический файл. Новые Tailwind-классы **не работают**.
Перед использованием любого класса проверить: `grep "класс" src/www/css/app.css`
Если класса нет — использовать `style="..."` (inline CSS).
Зафиксировано отсутствующие: `px-6`, `py-10`, `py-8`, `min-h-full`, `items-start`, **`p-6`**, **`border-t`**, **`border-neutral-*`**, **`space-y-2`**, **`space-y-4`**, **`space-y-6`**, **`hover:bg-*`** → нужен inline style.

**Замены для `p-6` (= 24px):**
- `p-6 pb-4` → `style="padding:24px 24px 16px;"` (header)
- `p-6 pt-4` → `style="padding:16px 24px 24px;"` (body/footer)
- `border-t dark:border-neutral-600` → `class="dark:border-neutral-600"` + `style="border-top-width:1px;"` (border-t отсутствует, цвет по умолчанию #e5e7eb, dark: класс переопределяет)
- `space-y-4` → `style="display:flex; flex-direction:column; gap:16px;"`
- `space-y-2` → `style="display:flex; flex-direction:column; gap:8px;"`

### Правильный паттерн для модалок — padding на оверлее:
```html
<!-- ПРАВИЛЬНО: padding на overlay + margin:0 auto на панели -->
<div v-if="showModal"
  class="fixed inset-0 bg-black bg-opacity-50 z-50 overflow-y-auto"
  style="padding:40px 24px;"
  @click.self="showModal = false">
  <div class="bg-white dark:bg-neutral-700 rounded-lg"
    style="max-width:520px; margin:0 auto;">
    <!-- контент -->
  </div>
</div>

<!-- НЕПРАВИЛЬНО — НЕ РАБОТАЕТ: -->
<!-- 1. flex wrapper + w-full → нет боковых отступов -->
<!-- 2. width:calc(100% - 48px) + margin:auto на панели → ненадёжно в fixed+overflow-y:auto контексте -->
```
`padding:40px 24px` на оверлее = гарантированно 24px слева/справа, 40px сверху/снизу.
`margin:0 auto` на панели = центрирование внутри контентной зоны оверлея.

---

## Правила работы

- Активная ветка: **`feature/kernel-module`** | Репо: `git@github.com:JohnnyVBut/awg-easy.git`
- Коммитить и пушить только в `feature/kernel-module`, не в worktree-ветки
- После каждого пуша напоминать команды деплоя на сервере
- Каждый коммит — подробное сообщение (что, почему, какие файлы)
- После завершения задачи обновлять `REQUIREMENTS.md` (статус) и этот файл

## Деплой на сервере

```bash
git pull origin feature/kernel-module
./build.sh
docker compose down && docker compose up -d
```

---

## 🚨 КРИТИЧЕСКИЕ ФИКСЫ — ПРОВЕРЯТЬ ПЕРЕД КАЖДЫМ РЕДАКТИРОВАНИЕМ

Это фиксы которые были добавлены после обнаружения реальных багов.
При редактировании соответствующих файлов — **убедиться что эти строки на месте**.

### FIX-1: iptables-nft + FORWARD в обоих направлениях + NAT только если disableRoutes=false
**Файл:** `src/lib/TunnelInterface.js` → метод `generateWgConfig()`
**Причина:** Ubuntu 22.04 использует nftables. FORWARD нужен -i И -o. NAT только для клиентских интерфейсов.

**ВАЖНО: PostUp использует `-A FORWARD` (append), НЕ `-I FORWARD` (insert).**
FirewallManager вставляет `FIREWALL_FORWARD` jump в позицию 1 при инициализации.
Если PostUp использует `-I FORWARD`, wg ACCEPT-правила вставляются перед `FIREWALL_FORWARD`
при каждом `restart()` (FIX-8) → весь трафик этого интерфейса обходит файрвол.
С `-A FORWARD` wg ACCEPT-правила всегда добавляются после `FIREWALL_FORWARD` → файрвол работает.

```javascript
// ПРАВИЛЬНО (disableRoutes=false — клиентский интерфейс):
config += `PostUp = ${getIsp}; iptables-nft -A FORWARD -i ${this.id} -j ACCEPT; iptables-nft -A FORWARD -o ${this.id} -j ACCEPT; iptables-nft -t nat -A POSTROUTING -s ${subnet} -o $ISP -j MASQUERADE\n`;
config += `PostDown = ${getIsp}; iptables-nft -D FORWARD -i ${this.id} -j ACCEPT 2>/dev/null || true; iptables-nft -D FORWARD -o ${this.id} -j ACCEPT 2>/dev/null || true; iptables-nft -t nat -D POSTROUTING -s ${subnet} -o $ISP -j MASQUERADE 2>/dev/null || true\n`;

// ПРАВИЛЬНО (disableRoutes=true — interconnect интерфейс, без NAT):
config += `PostUp = ${getIsp}; iptables-nft -A FORWARD -i ${this.id} -j ACCEPT; iptables-nft -A FORWARD -o ${this.id} -j ACCEPT; iptables-nft -t nat -A POSTROUTING -s ${subnet} -o $ISP -j MASQUERADE\n`;
config += `PostDown = ${getIsp}; iptables-nft -D FORWARD -i ${this.id} -j ACCEPT 2>/dev/null || true; iptables-nft -D FORWARD -o ${this.id} -j ACCEPT 2>/dev/null || true; iptables-nft -t nat -D POSTROUTING -s ${subnet} -o $ISP -j MASQUERADE 2>/dev/null || true\n`;

// НЕПРАВИЛЬНО: iptables (без -nft), только -i без -o, MASQUERADE при disableRoutes=true,
// или -I FORWARD (insert) вместо -A FORWARD (append) — ломает FIREWALL_FORWARD порядок
```

### FIX-2: Конфиг регенерируется перед каждым start() + down→up при "already exists"
**Файл:** `src/lib/TunnelInterface.js` → метод `start()` (~строки 336-358)
**Причина:** `--network host` → интерфейс переживает docker restart → "already exists".

```javascript
// ПРАВИЛЬНО:
async start() {
  await this.regenerateConfig(); // ← ВСЕГДА перед подъёмом
  try {
    await Util.exec(`${this._quickBin} up ${this.id}`);
  } catch (err) {
    if (err.message && err.message.includes('already exists')) {
      await Util.exec(`${this._quickBin} down ${this.id}`);
      await Util.exec(`${this._quickBin} up ${this.id}`);
    } else {
      throw err;
    }
  }
  this.data.enabled = true;
  await this.save();
}
// НЕПРАВИЛЬНО: сразу throw err при "already exists", или без regenerateConfig()
```

### FIX-3: stop() игнорирует "not a WireGuard/AmneziaWG interface"
**Файл:** `src/lib/TunnelInterface.js` → метод `stop()` (~строки 363-378)
**Причина:** Интерфейс уже был остановлен — это нормально, не должно крашить.

```javascript
// ПРАВИЛЬНО:
async stop() {
  try {
    await Util.exec(`${this._quickBin} down ${this.id}`);
  } catch (err) {
    if (!err.message.includes('is not a WireGuard interface') &&
        !err.message.includes('is not an AmneziaWG interface')) {
      throw err;
    }
    // молча игнорируем — уже остановлен
  }
  this.data.enabled = false;
  await this.save();
}
```

### FIX-4: Non-overlapping H1-H4 (4 зоны uint32, не случайные числа)
**Файл:** `src/lib/Settings.js` → функция `generateRandomHRanges()` (~строки 19-34)
**Причина:** H1-H4 не должны пересекаться. Uint32 делится на 4 равные зоны.

```javascript
// ПРАВИЛЬНО:
function generateRandomHRanges() {
  const RANGE_SIZE = 50_000_000;
  const ZONE_SIZE = Math.floor((0xFFFFFFFF - 5) / 4);
  const randRange = (zone) => {
    const zoneStart = 5 + zone * ZONE_SIZE;
    const zoneEnd = zoneStart + ZONE_SIZE - 1;
    const start = zoneStart + Math.floor(Math.random() * (zoneEnd - zoneStart - RANGE_SIZE));
    return `${start}-${start + RANGE_SIZE}`;
  };
  return { h1: randRange(0), h2: randRange(1), h3: randRange(2), h4: randRange(3) };
}
// НЕПРАВИЛЬНО: Math.random() * 0xFFFFFFFF без зон — могут пересечься
```

### FIX-5: Vue 2 — обновление массива только через splice
**Файл:** `src/www/js/app.js` → метод `_applyInterfaceUpdate()`
**Причина:** `array[idx] = newItem` не триггерит реактивность Vue 2.

```javascript
// ПРАВИЛЬНО:
_applyInterfaceUpdate(updatedIface) {
  const idx = this.tunnelInterfaces.findIndex(i => i.id === updatedIface.id);
  if (idx !== -1) {
    this.tunnelInterfaces.splice(idx, 1, updatedIface); // ← только splice!
  } else {
    this.tunnelInterfaces.push(updatedIface);
  }
},
// НЕПРАВИЛЬНО: this.tunnelInterfaces[idx] = updatedIface
```

### FIX-6: Address пира вычисляется из AllowedIPs + маска интерфейса
**Файл:** `src/lib/Peer.js` → метод `_generateCompleteConfig()` (~строки 173-177)
**Причина:** Поле `remoteAddress` удалено. Address = IP из AllowedIPs + маска iface.

```javascript
// ПРАВИЛЬНО:
if (this.allowedIPs && interfaceData.address) {
  const peerIp = this.allowedIPs.split('/')[0];
  const ifaceMask = interfaceData.address.split('/')[1] || '24';
  config += `Address = ${peerIp}/${ifaceMask}\n`;
}
// НЕПРАВИЛЬНО: this.remoteAddress (поле не существует)
```

### FIX-7: _quickBin / _syncBin выбирается по протоколу
**Файл:** `src/lib/TunnelInterface.js` → геттеры `_quickBin` и `_syncBin`
**Причина:** amneziawg-2.0 требует `awg-quick`/`awg`, wireguard-1.0 требует `wg-quick`/`wg`.

```javascript
get _quickBin() { return this.data.protocol === 'amneziawg-2.0' ? 'awg-quick' : 'wg-quick'; }
get _syncBin()  { return this.data.protocol === 'amneziawg-2.0' ? 'awg' : 'wg'; }
// НЕПРАВИЛЬНО: захардкодить только wg-quick или только awg-quick
```

### FIX-8: _kernelRemovePeer — AWG2 использует restart(), WG1 — wg set peer remove
**Файл:** `src/lib/TunnelInterface.js` → метод `_kernelRemovePeer()`
**Причина:** `awg set peer remove` дедлочится в AWG kernel module. Безопасное решение — restart().
Сериализован через `_reloadMutex`.

```javascript
// ПРАВИЛЬНО:
async _kernelRemovePeer(peerId, _publicKey) {
  if (!this.data.enabled) return;
  this._reloadMutex = this._reloadMutex
    .then(async () => {
      try {
        await this.restart();
        debug(`Interface ${this.id} restarted to remove peer ${peerId}`);
      } catch (err) {
        debug(`_kernelRemovePeer restart failed for ${peerId}: ${err.message}`);
      }
    })
    .catch(() => {});
  return this._reloadMutex;
}
// НЕПРАВИЛЬНО: awg set peer remove (дедлок), или без _reloadMutex
```

### FIX-9: _kernelSetPeer — AWG2 использует syncconf (reload), WG1 — wg set peer
**Файл:** `src/lib/TunnelInterface.js` → метод `_kernelSetPeer()`
**Причина:** `awg set peer <key> <params>` нестабилен в AWG kernel module — занимает
10-15+ секунд даже при добавлении первого пира на чистый интерфейс. После завершения
оставляет ядро в плохом состоянии → getStatus() дедлочится. Подтверждено в production.
Для AWG2: используем reload() (awg syncconf) — конфиг на диске уже обновлён раньше.
Сериализован через `_reloadMutex` (через reload()).

```javascript
// ПРАВИЛЬНО:
async _kernelSetPeer(peer) {
  if (!this.data.enabled) return;
  if (this.data.protocol === 'amneziawg-2.0') {
    return this.reload(); // awg syncconf — атомарно, без awg set peer
  }
  // WireGuard 1.0: wg set надёжен
  this._reloadMutex = this._reloadMutex
    .then(async () => { /* wg set peer ... */ })
    .catch(() => {});
  return this._reloadMutex;
}
// НЕПРАВИЛЬНО: awg set peer add для AWG2 (медленно, оставляет ядро в плохом состоянии)
```

**Важно для обоих методов (_kernelRemovePeer и _kernelSetPeer):**
Оба идут через `_reloadMutex` — гарантирует что `restart()` и `reload()` никогда
не выполняются одновременно. Конкурентный awg syncconf + awg-quick up = deadlock.

### FIX-10: Util.exec — timeout по умолчанию 30s
**Файл:** `src/lib/Util.js` → метод `exec()`
**Причина:** Без timeout зависший `awg`/`wg` процесс живёт вечно. При polling каждую
секунду накапливаются сотни зависших дочерних процессов → исчерпание ресурсов → container freeze.

```javascript
// ПРАВИЛЬНО:
static async exec(cmd, { log = true, timeout = 30000 } = {}) {
  // ...
  const child = childProcess.exec(cmd, { shell: 'bash', timeout, killSignal: 'SIGKILL' }, callback);
}
// getStatus() вызывает с timeout: 5000 — быстро убивает зависший awg show dump
// НЕПРАВИЛЬНО: без timeout (childProcess.exec висит вечно)
```

### FIX-11: ip -j (JSON флаг) зависает на некоторых ядрах Linux — НИКОГДА не использовать
**Файл:** `src/lib/RouteManager.js` — все методы работающие с `ip` командами
**Причина:** Флаг `-j` (JSON output) у `iproute2` использует другой путь через netlink API,
который на ряде конфигураций ядра Linux зависает **навсегда** (ни ответа, ни ошибки).
Подтверждено в production на Москве: `ip -j route show table main` висел бесконечно.
Контейнер с `--network host` делает polling каждую секунду → за несколько минут
накапливались десятки зависших процессов → `RouteManager.init()` не завершался →
все API-запросы к routing висели → UI показывал вечный Loading...

**Правило:** `ip -j` **ЗАПРЕЩЕНО** во всём проекте. Использовать только текстовый вывод + парсинг.

```javascript
// ПРАВИЛЬНО — текстовый вывод + _parseTextRoutes():
const out = await Util.exec('ip route show table main', { log: true, timeout: 5000 });
return RouteManager._parseTextRoutes(out || '');

// ПРАВИЛЬНО — ip rule show (текст) для обнаружения routing tables:
const out = await Util.exec('ip rule show', { log: false, timeout: 5000 });
// парсить строки вида "100: from all fwmark 0x1 lookup 100"

// ПРАВИЛЬНО — ip route get (текст):
const out = await Util.exec(`ip route get ${ip}`, { timeout: 5000 });
// парсить строку вида "10.8.0.5 dev wg0 src 10.8.0.1 uid 0"

// НЕПРАВИЛЬНО — НИКОГДА:
// ip -j route show table main  ← зависает
// ip -j route get 8.8.8.8      ← зависает
// ip -j rule show               ← зависает
// ip -j addr show               ← потенциально зависает
```

**Симптомы зависания `ip -j`:**
- docker logs показывает много повторяющихся `$ ip -j route show ...` без ответа
- UI показывает вечный "Loading..." при переходе на страницу Routing
- Через ~1 минуту (N×timeout) всё начинает работать — это таймауты Util.exec срабатывают

### FIX-12: HTTP method в fetch() — ВСЕГДА uppercase (Node.js 22 llhttp)
**Файл:** `src/www/js/api.js` → метод `call()` → `method: method.toUpperCase()`
**Причина:** Node.js 22 использует llhttp HTTP parser, который **строго требует** uppercase.
Fetch Standard нормализует GET/HEAD/POST/DELETE/OPTIONS/PUT, но **НЕ нормализует PATCH**.
`fetch(..., { method: 'patch' })` отправляет `patch` lowercase → llhttp отвергает с 400 + TCP RST
**до** попадания в h3/application код. Симптомы: `ERR_CONNECTION_RESET` + "в логах ничего нет".

```javascript
// ПРАВИЛЬНО: в call() — один раз покрывает все вызовы:
async call({ method, path, body }) {
  const res = await fetch(`./api${path}`, {
    method: method.toUpperCase(), // Node.js 22 llhttp: HTTP method must be uppercase
    ...
  });
}
// НЕПРАВИЛЬНО: method: 'patch', method: 'put', method: 'delete' (lowercase)
// Fetch Standard не нормализует PATCH — Node.js 22 отвергает на уровне HTTP парсера
```

### FIX-13: Порядок инициализации — InterfaceManager ПЕРВЫМ, затем RouteManager + NatManager
**Файлы:** `src/lib/Server.js` (конец конструктора), `src/lib/RouteManager.js`
**Причина (тройной баг):**

**Баг A** — `ip route add dev wgX` падает если wgX ещё не существует.

**Баг B** — `RouteManager.getInstance()` запускался как отдельная строка параллельно с
`InterfaceManager.getInstance()`. Маршруты добавлялись успешно (wgX оставался от предыдущего
запуска контейнера), но затем InterfaceManager делал `awg-quick down wgX` (FIX-2) →
ядро удаляло все маршруты → `awg-quick up wgX` → интерфейс без маршрутов.
JSON: enabled=true, ядро: маршрута нет → toggle → 500.

**Баг C** — Попытка разместить chain в начале конструктора (до регистрации route-handlers)
не работала — нужно размещать в КОНЦЕ конструктора, рядом с остальными eager init вызовами.

**Решение:**
- `RouteManager.init()`: только загружает JSON, маршруты в ядро НЕ применяет
- `RouteManager.restoreAll()`: применяет enabled маршруты — вызывается явно после InterfaceManager
- Server.js: chain `InterfaceManager → RouteManager.restoreAll() → NatManager.init()`
  (в конце конструктора, рядом с другими eager-init, НЕ в начале)
- start/restart handlers: `rm.reapplyForDevice(id)` для конкретного интерфейса

```javascript
// ПРАВИЛЬНО — в КОНЦЕ конструктора Server (после регистрации всех route handlers):
GatewayManager.getInstance()...;  // независим, параллельно

InterfaceManager.getInstance()
  .then(async () => {
    debug('InterfaceManager initialized successfully');
    // Интерфейсы подняты — безопасно применять маршруты
    const rm = await RouteManager.getInstance();  // init() = только JSON
    await rm.restoreAll();  // ip route add после того как интерфейсы существуют
    await NatManager.getInstance();  // iptables-nft правила
  })
  .catch(err => debug('Error initializing InterfaceManager:', err));

// ПРАВИЛЬНО — в start и restart handlers:
const rm = await RouteManager.getInstance();
await rm.reapplyForDevice(id).catch(err => debug(`reapplyForDevice(${id}) failed: ${err.message}`));

// НЕПРАВИЛЬНО:
// RouteManager.getInstance() как отдельная строка — запускается параллельно с InterfaceManager
// InterfaceManager chain в начале конструктора (до route handlers) — не работает
// Маршруты в RouteManager.init() — применяются до того как интерфейс существует
```

### FIX-14: NAT rules — идемпотентность через `iptables -C` (deduplication)
**Файл:** `src/lib/NatManager.js` → метод `_applyRule()`
**Причина:** При рестарте контейнера `--network host` → iptables-цепочки сохраняются в ядре.
Повторный `iptables-nft -t nat -A POSTROUTING ...` добавлял дублирующее правило.
Счётчики пакетов/байт у дублей обнулялись и путали статистику.

```javascript
// ПРАВИЛЬНО — проверка перед добавлением:
async _applyRule(rule) {
  const cmd = this._buildIptablesCmd(rule);
  // -C = check: exit 0 если правило есть, exit 1 если нет
  const exists = await Util.exec(cmd.replace(' -A ', ' -C '), { log: false })
    .then(() => true)
    .catch(() => false);
  if (!exists) {
    await Util.exec(cmd); // добавляем только если нет
  }
}
// НЕПРАВИЛЬНО: всегда -A POSTROUTING без проверки → дубли при рестарте
```

**Важно:** `-C` проверяет точное соответствие правила (source, target, interface).
Если правило изменилось (например, другой outInterface) — `-C` вернёт false → `-A` добавит новое.
Старое правило при этом останется — нужен явный `-D` при update/delete.

### FIX-15b: Gateway fallback — blackhole/default route при gateway down
**Файлы:** `src/lib/GatewayMonitor.js`, `src/lib/FirewallManager.js`
**Причина:** Firewall Rule с привязкой к gateway не реагировал на падение gateway — трафик уходил в никуда.

**Архитектура:**
- `GatewayMonitor` extends `EventEmitter`, при смене статуса `gateway emits 'statusChange'`
- `FirewallManager.init()` подписывается: `monitor.on('statusChange', _handleGatewayStatusChange)`
- `fallbackToDefault: bool` — флаг на каждом Firewall Rule (default: false)
- При `down`: если `fallbackToDefault=true` → `ip route replace default via <system-gw> table N`; иначе → `ip route replace blackhole default table N`
- При `up` (recovery): ждать 30s (anti-flap) → `ip route replace default via <rule.gateway.gatewayIP> table N`
- `_fallbackActive` (Set) + `_restoreTimers` (Map) — отслеживание состояния
- `_rebuildChains()` сбрасывает `_fallbackActive` + таймеры → GatewayMonitor re-emit при следующем polling
- `ip route replace` вместо `ip route add` — идемпотентно, не падает при stale route
- Группа: fallback только если ALL участники группы в статусе `down`
- System default gw: парсится из `ip route show default` (текст, без -j)

```javascript
// ПРАВИЛЬНО: ip route replace (idempotent)
await Util.exec(`ip route replace ${target} via ${gw} table ${tableN}`);
// НЕПРАВИЛЬНО: ip route add — падает если маршрут уже есть
```

### FIX-15: Ошибки `ip route` пробрасываются как HTTP 400 с деталью из stderr
**Файл:** `src/lib/RouteManager.js` → методы `addRoute()`, `toggleRoute(enable=true)`
**Причина:** Если `ip route add` падает (неверный prefix, шлюз недоступен, неверный интерфейс),
`childProcess.exec` бросает ошибку с `err.stderr` = сообщение из ядра.
Без обработки h3 возвращал 500 "Internal Server Error" — toast в UI был бесполезным.

```javascript
// ПРАВИЛЬНО — в addRoute() и toggleRoute():
try {
  await this._kernelAdd(route);
} catch (err) {
  const detail = (err.stderr || err.message || '').trim();
  throw createError({ status: 400, message: `ip route: ${detail}` });
}
// Frontend toast уже использует err.message — изменений не нужно.
// НЕПРАВИЛЬНО: пробрасывать ошибку без обёртки → 500 вместо 400
```

---

## Архитектура проекта

### Стек
- **Backend**: Node.js, h3 (HTTP framework), bcryptjs, QRCode, express-session
- **Frontend**: Vue 2 (CDN, не webpack), Tailwind CSS (CDN), VueI18n, ApexCharts
- **WireGuard**: `awg-quick` (AWG2) / `wg-quick` (WG1), `--network host`

### Хранилище данных (`/etc/wireguard/data/`)
```
/etc/wireguard/data/
  settings.json          ← глобальные настройки + AWG2 templates
  interfaces/
    wg10.json            ← data-plane interface
    wg11.json
  peers/
    wg10/
      {uuid}.json        ← peer данные
```

### Ключевые файлы backend

| Файл | Роль |
|------|------|
| `src/lib/Server.js` | HTTP сервер (h3), все API маршруты |
| `src/lib/Settings.js` | Singleton: global settings + AWG2 templates |
| `src/lib/InterfaceManager.js` | Singleton: управление всеми data-plane интерфейсами |
| `src/lib/TunnelInterface.js` | Один WG/AWG интерфейс (start/stop/config/peers) |
| `src/lib/Peer.js` | Модель пира, генерация клиентского конфига и QR |
| `src/lib/WireGuard.js` | Старый wg0 (вкладка Clients, **не трогать**) |
| `src/www/js/api.js` | Все клиентские API методы |
| `src/www/js/app.js` | Vue app: data + methods |
| `src/www/index.html` | Весь UI (один файл, Vue template) |

### Дополнительные технические решения
- Маска пира всегда `/32` (AllowedIPs = "10.x.x.x/32")
- Интерфейсы нумеруются с wg10 (wg10, wg11, ...), порты с 51830
- H1-H4 хранятся как строки `"start-end"` — диапазоны одинаковые на обеих сторонах туннеля. Рандомизация внутри диапазона — задача AWG протокола, приложение её не выполняет
- Приватный ключ хранится на сервере (нужен для QR/download клиентов)

---

## API маршруты (новая архитектура)

```
GET/PUT  /api/settings
GET/POST /api/templates
GET/PUT/DELETE /api/templates/:id
POST /api/templates/:id/set-default
POST /api/templates/:id/apply          ← возвращает AWG2 params с fresh H1-H4
POST /api/templates/generate           ← генерация AWG2 параметров { profile, intensity, host, saveName? }

GET/POST /api/tunnel-interfaces
GET/PATCH/DELETE /api/tunnel-interfaces/:id
POST /api/tunnel-interfaces/:id/start  ← возвращает { interface: iface.toJSON() }
POST /api/tunnel-interfaces/:id/stop   ← возвращает { interface: iface.toJSON() }
POST /api/tunnel-interfaces/:id/restart ← возвращает { interface: iface.toJSON() }
GET /api/tunnel-interfaces/:id/export-params  ← { name, publicKey, endpoint, address, protocol, [presharedKey] }

GET/POST /api/tunnel-interfaces/:id/peers
POST /api/tunnel-interfaces/:id/peers/import-json  ← создать Interconnect peer из JSON
GET/PATCH/DELETE /api/tunnel-interfaces/:id/peers/:peerId
GET /api/tunnel-interfaces/:id/peers/:peerId/config
GET /api/tunnel-interfaces/:id/peers/:peerId/qrcode.svg
POST /api/tunnel-interfaces/:id/peers/:peerId/enable
POST /api/tunnel-interfaces/:id/peers/:peerId/disable
GET /api/tunnel-interfaces/:id/export-obfuscation         ← AWG2 params JSON

GET    /api/routing/table          ← kernel routes (по таблице, default=main)
GET    /api/routing/tables         ← список routing tables (из ip rule show)
GET    /api/routing/test           ← route test: { ip[, src] } → { result, matchedRule, steps }
                                    если src указан: simulateTrace (FirewallManager PBR rules) →
                                    matchedRule: { id, name, fwmark } | null
GET    /api/routing/routes         ← статические маршруты (из routes.json)
POST   /api/routing/routes         ← создать { destination, via, dev, metric, table, ... }
PATCH  /api/routing/routes/:id     ← обновить | toggle: { enabled: bool }
DELETE /api/routing/routes/:id     ← удалить

GET    /api/nat/interfaces        ← список сетевых интерфейсов хоста (ip -o link show)
GET    /api/nat/rules             ← список NAT правил
POST   /api/nat/rules             ← создать правило { name, source, outInterface, type, toSource, comment }
PATCH  /api/nat/rules/:id         ← обновить правило | toggle: { enabled: bool }
DELETE /api/nat/rules/:id         ← удалить правило

GET/POST /api/gateways                    ← CRUD шлюзов + статус мониторинга
GET/PATCH/DELETE /api/gateways/:id
GET/POST /api/gateway-groups              ← CRUD групп шлюзов
GET/PATCH/DELETE /api/gateway-groups/:id

GET    /api/aliases               ← список алиасов
POST   /api/aliases               ← создать { name, type, entries }
GET/PATCH/DELETE /api/aliases/:id
POST   /api/aliases/:id/upload    ← загрузить файл префиксов → ipset
POST   /api/aliases/:id/generate  ← сгенерировать через prefixes.py { country?, asn?, asnList? }

GET    /api/firewall/interfaces   ← список интерфейсов хоста (для поля interface)
GET    /api/firewall/rules        ← список правил (sorted by order)
POST   /api/firewall/rules        ← создать правило { interface, protocol, source, destination, action, gatewayId, ... }
PATCH  /api/firewall/rules/:id    ← обновить или toggle { enabled: bool }
DELETE /api/firewall/rules/:id    ← удалить правило
POST   /api/firewall/rules/:id/move ← { direction: 'up'|'down' }
```

---

## UI Навигация — Sidebar + Per-Page Routing

**Архитектура:** Боковое меню (sidebar) + `activePage` переключает контент `<main>`.
Старые горизонтальные табы (`activeTab`) удалены. WAN Tunnels удалены полностью.

| Страница | Ключ `activePage` | Статус |
|----------|------------------|--------|
| Interfaces | `'interfaces'` | ✅ динамические вкладки, per-interface view (info + peers) |
| Gateways | `'gateways'` | ✅ CRUD + live мониторинг (ping/latency/loss) + Gateway Groups |
| Routing | `'routing'` | ✅ Status (kernel routes + route test) + Static routes CRUD + OSPF placeholder |
| NAT | `'nat'` | ✅ Outbound NAT CRUD + toggle + Port Forwarding placeholder |
| Firewall → Aliases | `'firewall-aliases'` | ✅ CRUD host/network/ipset + upload + generate (prefixes) |
| Firewall → Rules | `'firewall'` | ✅ CRUD + ACCEPT/DROP/REJECT + PBR (gateway) + ↑↓ order |
| Settings | `'settings'` | ✅ Global Settings + AWG2 Templates + Generate (⚡) |
| Administration | `'administration'` | ✅ Admin Tunnel (бывший Clients tab) |

---

## Что сделано (хронология коммитов)

| Коммит | Ветка | Что |
|--------|-------|-----|
| `de31c42` | feature/kernel-module | fix: iptables-nft + FORWARD ACCEPT в PostUp/PostDown |
| `8892179` | feature/kernel-module | fix: регенерация конфига + down→up при "already exists" |
| `359984e` | feature/kernel-module | fix: H1-H4 как non-overlapping ranges |
| `63a7a18` | feature/kernel-module | refactor: убран remoteAddress, Address вычисляется из AllowedIPs |
| `c83b983` | feature/kernel-module | feat: Settings.js + Settings/Templates API |
| `7482fc2` | feature/kernel-module | feat(ui): вкладка Settings (Global Settings + AWG2 Templates) |
| `aa4feda` | feature/kernel-module | fix(ui): reactive status update after start/stop/restart |
| `f8ca1ab` | feature/kernel-module | feat(ui): S2S badge + runtimeEndpoint в карточке пира |
| `a3d0aa5` | feature/kernel-module | fix: interconnect peer allowedIPs = host /32, не подсеть |
| `028a7c5` | feature/kernel-module | fix: mutex для _kernelSetPeer + exec timeout |
| `b3c53be` | feature/kernel-module | fix: AWG2 _kernelSetPeer → awg syncconf вместо awg set peer |
| `337869e` | feature/kernel-module | feat(ui): dashboard view — все пиры всех интерфейсов на одном экране |
| `943a046` | feature/kernel-module | feat(ui): Edit Interface modal (имя, адрес, порт, AWG2 профиль) |
| `075ebc8` | feature/kernel-module | fix(ui): загрузка templates после логина (AWG2 дропдаун без визита Settings) |
| `138e33d` | feature/kernel-module | feat: Routing page — Status + Static Routes + OSPF placeholder (RouteManager, API, UI) |
| `152f010` | feature/kernel-module | fix: routing table discovery via kernel (ip rule show) instead of container rt_tables |
| `2f025c3` | feature/kernel-module | fix: add iproute2 explicitly to Dockerfile |
| `36b3191` | feature/kernel-module | fix(ui): show kernelRoutesError + loading indicator |
| `ef5d5cc` | feature/kernel-module | fix(ui): parallel loading + kernelRoutesLoading state |
| `b3c7153` | feature/kernel-module | fix: remove ALL ip -j usage — text parsing instead (hangs on some kernels) |
| `dfc34c8` | feature/kernel-module | fix: toJSON() missing settings + api.js json error + Server.js try-catch |
| `1579be6` | feature/kernel-module | fix: uppercase HTTP methods in api.js (Node.js 22 llhttp rejects lowercase) |
| `d75d7d5` | feature/kernel-module | feat(ui): toast notification system — replace all alert() with toasts |
| `a0a41bf` | feature/kernel-module | feat: NAT page — Outbound Source NAT CRUD (NatManager, API, UI) |
| `09df40e` | feature/kernel-module | fix: RouteManager eager init — static routes survive container restart |
| `8f53a48` | feature/kernel-module | fix: static routes persist after restart + toggleRoute 500 fix + reapplyForDevice() |
| `97d64ff` | feature/kernel-module | fix: routes restored correctly after restart (FIX-13 v3 — тройной баг) |
| `f085bfa` | feature/kernel-module | fix(routing): ip route kernel errors propagated as HTTP 400 with detail |
| `d40d56b` | feature/kernel-module | fix: NAT rule deduplication via iptables -C check, preserves packet counters |
| `47e91bf` | feature/kernel-module | fix: NAT rules deduplicated on container restart (idempotent _applyRule) |
| `25948d6` | feature/kernel-module | chore: prefixes.py — вспомогательный скрипт агрегации префиксов |
| `1b54fd8` | feature/kernel-module | feat: AWG2 parameter generator — порт AmneziaWG-Architect (AwgParamGenerator.js + API + UI) |
| `fc0df23` | feature/kernel-module | fix(generator): remove `<c>` tag from all CPS signatures |
| `3be4389` | feature/kernel-module | feat: Firewall Aliases + Policy-Based Routing (PBR) |
| `18930a3` | feature/kernel-module | fix(ipset): replace python3 dependency with pure Node.js PrefixFetcher |
| `0b59bb2` | feature/kernel-module | fix(ui): alias auto-generate on create — save genOpts before form reset |
| `f90eea5` | feature/kernel-module | fix(ui): alias table — show 'empty' instead of '0 prefixes' + correct generate indicator |
| `ab1b9cb` | feature/kernel-module | fix(api): alias/policy CRUD — fix id mismatch + upload JSON instead of multipart |
| `faf13f4` | feature/kernel-module | refactor(ui): move Upload button into Edit Alias modal |
| `2025867` | feature/kernel-module | feat(ipset): add CIDR aggregation to PrefixFetcher (collapse_addresses) |
| `57a9352` | feature/kernel-module | fix(ui): alias edit modal — restore genSource from saved generatorOpts |
| `f639ffa` | feature/kernel-module | fix(ui): restore public IP display for client peers |
| `edec89a` | feature/kernel-module | docs: add English version of API reference (docs/API.en.md) |
| `b566fce` | feature/kernel-module | docs: require API docs update in both languages for every new endpoint |
| `1bdbd8f` | feature/kernel-module | docs: add low-priority wishlist item — UI config writable via API |
| `c488cca` | feature/kernel-module | feat: Firewall Rules — unified filter + PBR (replaces PolicyManager) |
| `343fabf` | feature/kernel-module | feat(gateways): HTTP(S) reachability monitoring + health decision rules |
| `7a6b225` | feature/kernel-module | fix(gateways): HTTP probe starts by monitorRule, not monitorHttp.enabled |
| `4d8a693` | feature/kernel-module | fix(gateways): HTTP window auto-expands to fit MIN_PROBES at given interval |
| `ea90153` | feature/kernel-module | fix(docker): add curl to apk packages — required for HTTP gateway monitoring |
| `6257d80` | feature/kernel-module | fix(gateway-monitor): replace curl subprocess with native Node.js http/https for HTTP probes |
| `f6bac16` | feature/kernel-module | feat(gateway-monitor): expose HTTP response code in status + debug logging |
| `17188eb` | feature/kernel-module | fix(gateway-monitor): reduce default HTTP probe interval from 60s to 10s |
| `bfebc98` | feature/kernel-module | feat: gateway fallback — blackhole or default route when gateway goes down |
| `bb2742e` | feature/kernel-module | feat(aliases): add group type — combines multiple host/network aliases |
| `fb8f83a` | feature/kernel-module | feat: L4 port aliases — port/port-group types + firewall rule port matching |
| `85cc147` | feature/kernel-module | refactor(ui): extract repeated inline styles to CSS utility classes |
| `96ed64f` | feature/kernel-module | fix(ui): rename port mode button None → Any (semantically correct) |
| `f1e02ac` | feature/kernel-module | fix(ui): add missing px-2/py-1 Tailwind classes — alias badges had no padding |
| `37f18a8` | feature/kernel-module | feat(nat): show auto MASQUERADE rules from tunnel interfaces in NAT tab |
| `00b9fc0` | feature/kernel-module | fix(nat): use getAllInterfaces() instead of non-existent getAll() |
| `c722be5` | feature/kernel-module | feat(nat): alias support in NAT rules source field |

---

## Go Rewrite (feature/go-rewrite)

### Стек
- **Backend:** Go 1.23, Fiber v2, modernc.org/sqlite (CGO-free), embed.FS
- **Frontend:** тот же Vue 2 / Tailwind / VueI18n, вшит в бинарник через `//go:embed all:www`
- **Конфиг:** флаги CLI + ENV-переменные (флаг приоритетнее)

### Ключевые архитектурные решения

| Решение | Подробности |
|---------|-------------|
| embed.FS | Фронтенд вшит в бинарник при сборке. Static middleware регистрируется **после** всех `/api/*` маршрутов — иначе SPA-fallback глотает API-запросы |
| Fiber middleware порядок | recover → logging → api routes → static(SPA fallback) |
| Nil slice → JSON null | Go `nil` slice сериализуется как `null`, не `[]`. Везде инициализируем пустым slice перед `c.JSON()` |
| Compat layer | `internal/api/compat.go` — заглушки для старых Node.js эндпоинтов, которые фронтенд всё ещё вызывает |
| Логирование | Кастомный middleware: логируются только мутации (POST/PATCH/DELETE/PUT) и ошибки (4xx/5xx). GET 200 не логируются — они случаются каждую секунду из setInterval и спамили бы лог |
| SQLite | Всё хранится в SQLite (`/etc/wireguard/data/awg.db`), JSON-файлы Node.js версии не используются |

### Коммиты feature/go-rewrite (эта сессия)

| Коммит | Что |
|--------|-----|
| `003ed2e` | feat(go): embed.FS — фронтенд вшит в бинарник |
| `7f50489` | fix(frontend): static middleware регистрируется ПОСЛЕ api routes |
| `d5e690a` | fix(api): compat shims + nil-slice JSON fixes |
| `514084a` | fix(compat): /api/release возвращает 999999 (подавляет баннер "update available") |
| `8909432` | fix(api): все list-эндпоинты оборачиваются в именованные ключи (contract с фронтендом) |
| `b82baf0` | fix(logging): кастомный middleware вместо Fiber logger — только мутации и ошибки |
| `b75c4f1` | fix(build): DOCKER_BUILDKIT=1 в build-go.sh (старый Docker без buildx) |
| `1b8b6ab` | feat(api): 7 пропущенных эндпоинтов из Node.js версии (name/address/expireDate/generateOneTimeLink/export-json/backup/restore) |
| `1637848` | fix(aliases): добавлен GET /aliases/:id/generate/:jobId — эндпоинт статуса джоба |
| `a035f81` | fix(aliases): race condition — watchJob обновляет DB после фронтенд-поллинга (FinalizeGeneration) |
| `8ef5b12` | docs: sync CLAUDE.md — Go rewrite session |
| `9aee3e6` | fix(routing): route test — SimulateTrace + fwmark instead of 'from' flag (FIX-GO-8) |
| `8652c52` | docs: add FIX-GO-8 + update checkpoint |
| `f1e6ab0` | fix(firewall): PBR routing table empty after container restart (FIX-GO-9) |
| `6bcb3ec` | fix(firewall): ipInCIDR always false for non-network-address IPs (FIX-GO-10) |
| `a87aeba` | fix(routing): ipInCIDR — replace broken bitmask with net.ParseCIDR (FIX-GO-10 deploy) |
| `7cc4675` | fix(ui): firewall action badge grayed out when rule disabled — correct path internal/frontend/www |
| `63d4016` | fix(api): backup includes PSK for all peer types |
| `f1812be` | fix(s2s): populate peer address from export — required for multi-peer transit |
| `e80e9a6` | fix(ui): rebrand AWG Easy → WG Router (title + sidebar) |
| `275335b` | fix(peers): stable sort — GetAllPeers map iteration was non-deterministic (FIX-GO-13) |
| `192eb4b` | feat: add --bind / BIND_ADDR to restrict Web UI listen address |
| `6beed08` | feat(deploy): Caddy reverse proxy — decoy site + hidden admin path + rate limiting |
| `685061b` | fix(caddy): use WIRESTEER_PORT env var instead of hardcoded 51821 |
| (pending) | fix(caddy): standalone mode for first cert issuance + README + CLAUDE.md |

### API contract — обёртка ответов

Фронтенд ВСЕГДА использует `res.key || []` паттерн. Go обязан оборачивать:

| Эндпоинт | Ключ обёртки |
|---|---|
| GET /tunnel-interfaces | `{ interfaces: [...] }` |
| GET .../peers | `{ peers: [...] }` |
| POST .../peers | `{ peer: {...} }` |
| POST .../peers/import-json | `{ peer: {...} }` |
| POST .../peers/:id/enable/disable | `peer` (bare object, OK) |
| GET /routing/table | `{ routes: [...] }` |
| GET /routing/tables | `{ tables: [...] }` |
| GET /routing/routes | `{ routes: [...] }` |
| GET /routing/test | `{ result, matchedRule: null, steps: [] }` |
| GET /nat/interfaces | `{ interfaces: [...] }` |
| GET /nat/rules | `{ rules: [...] }` |
| GET /gateways | `{ gateways: [...] }` |
| GET /gateway-groups | `{ groups: [...] }` |
| GET /system/interfaces (compat) | `{ interfaces: [...] }` |

### Известные баги / особенности Go rewrite

**FIX-GO-1: nil slice → JSON null**
Go `nil` slice сериализуется как `null`. `null.peers` в JS → TypeError.
Всегда: `if slice == nil { slice = []Type{} }` перед `c.JSON()`.

**FIX-GO-2: Static middleware после API routes**
Если `filesystem.New()` регистрировать до `/api/*`, все API-запросы отдают `index.html`.
Порядок: сначала все `api.*` роуты, потом `app.Use("/", filesystem.New(...))`.

**FIX-GO-3: Alias generation race condition**
`ipset.runGeneratorAsync` ставит статус `"done"` в `jobs` sync.Map.
`aliases.watchJob` спит 2s потом обновляет DB.
Фронтенд поллит каждые 3s → может получить `"done"` до обновления DB → `loadAliases()` видит `entryCount=0`.
Фикс: `getAliasJobStatus` вызывает `FinalizeGeneration(aliasID, entryCount)` перед ответом — DB обновляется синхронно.

**FIX-GO-4: GET /api/wireguard/client спамит лог каждую секунду**
Фронтенд вызывает `refresh()` → `getClients()` каждую секунду.
Без compat-заглушки → SPA fallback → HTML → JSON parse fail → "Server error 200: OK".
Фикс: `GET /wireguard/client → []` в `RegisterCompatAuth()`.

**FIX-GO-5: Fiber logger логирует GET 200 поллинг**
Fiber `fiberlog.New()` логирует каждый запрос.
Фронтенд делает setInterval 1s → `/api/wireguard/client` + `/api/tunnel-interfaces/*/peers` каждую секунду.
Фикс: кастомный middleware, логирует только `method != "GET" || status >= 400`.

**FIX-GO-6: /api/release возвращает 0 → баннер "Update available"**
Старый фронтенд сравнивает версию с changelog. `0 < 14` → красный баннер на всех вкладках.
Фикс: `/api/release → 999999`.

**FIX-GO-7: Alias job status polling — missing endpoint**
Фронтенд поллит `GET /aliases/:id/generate/:jobId` каждые 3s.
Эндпоинт отсутствовал → 404 → catch → clearInterval → "empty" без тоста об ошибке.
Фикс: добавлен `GET /:id/generate/:jobId → getAliasJobStatus`.

**FIX-GO-8: Route test — "ip route get from <non-local>" → Network unreachable**
При лукапе с `src=192.168.100.3` (адрес не принадлежит локальному интерфейсу контейнера)
`ip route get <dst> from <src>` возвращает "RTNETLINK answers: Network unreachable".
В Node.js `from` никогда не использовался — вместо него `simulateTrace(src, dst)` находил
fwmark из правил файрвола, затем `ip route get <dst> mark <fwmark>` работал корректно.

Фикс: `testRoute` handler теперь делает:
1. `firewall.SimulateTrace(src, dst)` — находит первое совпадающее PBR-правило
2. Если правило с fwmark найдено → `ip route get <dst> mark <fwmark>` (policy table)
3. Если правило без fwmark или не найдено → `ip route get <dst>` (default table)
Ответ теперь содержит реальные `matchedRule` и `steps` (не null/[]).

`routing.TestRoute` упрощён: убран параметр `srcIP string` — PBR трассировка
теперь ответственность handler'а, а не менеджера маршрутов.

**FIX-GO-9: PBR routing table empty after container restart**
Два бага:
- **Init order**: `firewall.Init()` (шаг 4) вызывает `rebuildChains()` → `ip route replace default via X dev wgY table N`. Но wgY ещё не существует — `tunnel.Init()` на шаге 5. Команда падает тихо, таблица N остаётся пустой. ip rule сохраняется от предыдущего запуска (`--network host`), пакеты маркируются → lookup N → пусто → проваливаются в main table → неверный шлюз.
- **Interface restart**: `wg-quick down` удаляет ВСЕ маршруты интерфейса включая `default via X dev wgY table N`. После рестарта никто не восстанавливал маршрут.

Фикс:
1. `RebuildChains()` — публичный wrapper для rebuildChains
2. `main.go`: `fwMgr.RebuildChains()` вызывается ПОСЛЕ `tunnel.Init()`
3. `interfaces.go`: `firewall.Get().RebuildChains()` в `startInterface` и `restartInterface`
4. `onlink` во всех `ip route replace` — обходит проверку досягаемости next-hop (нужно для шлюзов не в подсети интерфейса)

**FIX-GO-10: ipInCIDR всегда false для IP не совпадающих с сетевым адресом**
`bits.RotateLeft32(^uint32(0), -prefixLen)` — ротация всех единиц на любое число позиций = снова все единицы = `0xFFFFFFFF`. Маска никогда не применялась корректно. Результат: `192.168.100.3 & 0xFFFFFFFF != 192.168.100.0 & 0xFFFFFFFF` → false. SimulateTrace CIDR-матчинг был полностью сломан — "No rule matched" для любого src IP кроме сетевого адреса.

Фикс: заменить на `net.ParseCIDR(cidr)` + `ipNet.Contains(ip)` — stdlib правильно применяет маску.

**FIX-GO-11: importPeerJSON не сохраняет address пира для transit-интерфейсов**
Для transit-пиров (`allowedIPs=0.0.0.0/0`) `AddPeer` пропускает деривацию `address` из AllowedIPs (guard `peerIP != "0.0.0.0"`). `importPeerJSON` использовал `body["address"]` только как фоллбэк для AllowedIPs /32, но никогда не писал в `inp.Address`. Итог: `address=""` у всех импортированных interconnect-пиров. На интерфейсе с 3+ пирами (full mesh) все пиры неотличимы в UI — у всех `allowedIPs=0.0.0.0/0` и пустой адрес.

Фикс: `importPeerJSON` явно читает `body["address"]` в `inp.Address` до обработки `allowedIPs`.

**FIX-GO-12: Фронтенд Go rewrite — файлы в internal/frontend/www/, не src/www/**
Go rewrite вшивает фронтенд из `internal/frontend/www/` (`//go:embed all:www` в `internal/frontend/embed.go`). `src/www/` — файлы Node.js версии, не попадают в Go-бинарник. Все изменения фронтенда для Go rewrite делать ТОЛЬКО в `internal/frontend/www/`.

**FIX-GO-13: GetAllPeers — Go map iteration non-deterministic → dashboard reorders every second**
`t.peers` — `map[string]*peer.Peer`. Go runtime намеренно рандомизирует порядок итерации по map при каждом обходе. `GetAllPeers()` итерировал map напрямую → каждый вызов возвращал пиров в другом порядке → фронтенд (polling 1s) перемешивал карточки пиров в dashboard каждую секунду.
`ORDER BY created_at` в SQL-запросе `GetPeers` не помогал — `listPeers` API читает из in-memory map, а не напрямую из SQLite.
Фикс: `sort.Slice(out, func(i,j int) bool { return out[i].CreatedAt < out[j].CreatedAt })` в `GetAllPeers()`. RFC3339 строки сортируются лексикографически = хронологически.

### Compat layer (internal/api/compat.go)

**RegisterCompat** (без авторизации):
- `/lang` → `"en"`
- `/release` → `999999`
- `/remember-me` → `true`
- `/ui-traffic-stats` → `false`
- `/ui-chart-type` → `0`
- `/wg-enable-one-time-links` → `false`
- `/ui-sort-clients` → `false`
- `/wg-enable-expire-time` → `false`
- `/ui-avatar-settings` → `{dicebear:null, gravatar:false}`

**RegisterCompatAuth** (требует авторизации):
- `GET /wireguard/client` → `[]` (пустой список — фронтенд поллит каждую секунду)
- `ALL /wireguard/*` → 501 Not Implemented
- `GET /system/interfaces` → `{interfaces: [...]}`  (для NAT dropdown)

### FIX-GO-16: doReload() — 10s timeout + fallback на Restart() при дедлоке syncconf
**Файл:** `internal/tunnel/interface.go` → `doReload()`
**Причина:** amneziawg-installer v5.7.5 выпустил митигацию для upstream AWG kernel deadlock #146.
Без timeout `awg syncconf` может зависнуть навсегда (deadlock в kernel module).
С fallback — при ошибке делается полный `Restart()` (down→up) вместо зависания.

```go
// ПРАВИЛЬНО: 10s timeout + fallback на Restart()
const syncconfTimeout = 10 * time.Second

func (t *TunnelInterface) doReload() error {
    cmd := fmt.Sprintf("%s syncconf %s <(%s strip %s)", ...)
    if _, err := util.Exec(cmd, syncconfTimeout, true); err != nil {
        log.Printf("tunnel: %s syncconf failed (%v) — falling back to full restart", t.ID, err)
        if restartErr := t.Restart(); restartErr != nil {
            return fmt.Errorf("syncconf failed and fallback restart also failed: %w", restartErr)
        }
        return nil
    }
    return nil
}
// НЕПРАВИЛЬНО: util.ExecDefault (30s без fallback) — при deadlock зависает на 30s, не восстанавливается
```

---

### Checkpoint Go rewrite

**Активная ветка:** `feature/go-rewrite`
**Последний коммит:** `eef0ddd` security: fix SameSite=Strict, add input validation, document threat model

**Что работает (протестировано на production):**
- Interfaces: CRUD, start/stop/restart, peers, S2S interconnect, export-params, backup/restore
- Peers: полный CRUD + name/address/expireDate/oneTimeLink/export-json
- Routing: static routes + kernel routes + routing tables + policy-aware Route Lookup (SimulateTrace)
- NAT: Outbound MASQUERADE/SNAT CRUD + alias source + auto-правила
- Gateways: CRUD + live ping/HTTP monitoring + Gateway Groups + fallback
- Firewall Aliases: host/network/ipset/group + L4 port/port-group + upload + generate (async job)
- Firewall Rules: ACCEPT/DROP/REJECT + PBR (gateway) + port matching + ↑↓ order
- UI: disabled rule → серый ACCEPT/REJECT/DROP badge
- AWG2 Templates: CRUD + Generate (7 CPS-профилей)
- Auth: session cookie, bcrypt
- **Caddy reverse proxy** (`deploy/caddy/`): HTTPS/HTTP3, hidden admin path, rate limit, decoy site, security headers, BIND_ADDR=127.0.0.1
- **TLS cert**: acme.sh shortlived profile (6-day), bare IP via Let's Encrypt, первый выпуск через standalone mode

**Caddy — статус деплоя на production:**
- ✅ acme.sh standalone mode — сертификат выдан Let's Encrypt для bare IP
- ✅ `.env` создан с рандомным `ADMIN_PATH`
- ✅ WireSteer перезапущен с `BIND_ADDR=127.0.0.1`
- ✅ decoy.mp4 скачан (Big Buck Bunny)
- ⏳ `docker compose up -d --build` в `deploy/caddy/` — следующий шаг
- ⏳ Проверить `https://<IP>/<ADMIN_PATH>/`

**acme.sh — модель обновления (задокументирована):**
```
Первый выпуск: acme.sh --standalone   (порт 80 занимает сам, Caddy не нужен)
Продление:     acme.sh --webroot /srv/acme  (Caddy отвечает на challenge)
```
Renewal hook: `docker exec wiresteer-caddy caddy reload --config /etc/caddy/Caddyfile`

**Что не реализовано:**
- Admin Tunnel (wg0) — заглушка 501
- Port Forwarding (DNAT)
- One-time links (generateOneTimeLink сохраняет токен, но `/cnf/:link` не реализован)
- Backup с приватным ключом интерфейса — решено добавить checkbox "Include private keys" в UI (по умолчанию выкл). Без него restore восстанавливает только пиров, интерфейс оставляет текущий ключ.

**Хотелки (обсуждено 2026-03-20, не запланировано к реализации):**

1. **VPN-only management + INPUT chain в UI**
   - После деплоя закрыть веб-интерфейс и SSH из интернета
   - Доступ только через VPN с конкретных IP
   - Сейчас: делается вручную через iptables
   - Хотелось бы: поддержка INPUT chain в FirewallManager → управление из UI

2. **Multi-user + Resource-scoped RBAC**
   - Таблицы: users, groups, user_groups, group_access (group_id, resource_type, resource_id, actions[])
   - Пример: группа "kz-ops" видит и редактирует только пиров на wg10
   - Роли: superadmin (*:*), operator (peers:read+write на своих интерфейсах), viewer (read-only)
   - Каждый handler проверяет доступ к конкретному resource_id
   - GET /tunnel-interfaces и GET /peers фильтруются по доступным интерфейсам
   - Отдельный UI раздел управления пользователями/группами
   - Оценка: 3-4 недели работы

3. **TOTP 2FA (Google Authenticator)**
   - Второй фактор при логине (RFC 6238)
   - QR-код при создании пользователя
   - Не зависит от внешних сервисов (в отличие от Telegram OTP)
   - Предпочтительнее Telegram OTP как основной 2FA

4. **Telegram OTP (альтернативный 2FA)**
   - Логин: username → OTP отправляется в Telegram → вводится в форме
   - users.telegram_id + in-memory OTP store с TTL 5 минут
   - Зависит от доступности Telegram с хоста (проблема для России)
   - Fallback: консольная команда `wiresteer otp <username>` для генерации OTP напрямую
   - Требует OUTPUT chain routing для Telegram через KZ gateway (см. ниже)

5. **Telegram Bot для управления роутером**
   - Отдельный микросервис (не встроен в основной бинарник)
   - Аутентифицируется в WireSteer API как bot-пользователь с ролью operator
   - Whitelist chat_id — только конкретные Telegram ID могут давать команды
   - Ограниченный набор команд: статус интерфейсов, restart, просмотр пиров (не удаление/изменение firewall)
   - Bot token в ENV
   - Все действия логируются
   - **Требует:** OUTPUT chain PBR для Telegram IP (mangle OUTPUT → fwmark → KZ gateway)
     ```bash
     iptables-nft -t mangle -A OUTPUT -m set --match-set telegram_set dst -j MARK --set-mark <fwmark>
     ```
     telegram_set = алиас типа network с префиксами: 149.154.160.0/20, 91.108.4.0/22, 91.108.8.0/22, 91.108.16.0/22, 91.108.56.0/22

**S2S топология — важные ограничения (задокументировано 2026-03-19):**
- WireGuard использует `allowedIPs` как таблицу маршрутизации исходящего трафика
- `allowedIPs=0.0.0.0/0` работает только для **одного** пира на интерфейсе — при двух+ пирах с одинаковым prefix WG выберет только один
- Full mesh из N роутеров: каждый пир должен иметь `/32` конкретного соседа + нужные префиксы за ним
- Настоящий LAN Exchange (L2 shared medium) через WireGuard невозможен — только point-to-point туннели
- Рекомендуемые топологии: hub-and-spoke (спицы с `0.0.0.0/0`, хаб с `/32`) или full mesh с явными prefix-листами

---

## Checkpoint (текущее состояние)

**Активная ветка:** `feature/kernel-module`
**Последний коммит:** `586a515` fix(routing): simulateTrace uses FirewallManager (not PolicyManager)

**Что готово (протестировано на production):**
- Interfaces: CRUD, start/stop, peers, S2S interconnect, dashboard
- Routing: static routes + status + policy-aware Route Lookup (src→dst + PBR trace)
- NAT: Outbound MASQUERADE/SNAT CRUD + alias source + auto правила от интерфейсов
- Gateways: CRUD + live ping/HTTP monitoring + Gateway Groups + fallback при down
- Firewall Aliases: host/network/ipset/group + L4 port/port-group + upload + generate (RIPE)
- Firewall Rules: ACCEPT/DROP/REJECT + PBR (gateway) + port alias matching + ↑↓ order
- AWG2 Templates: CRUD + Generate (7 CPS-профилей)

---

### ✅ Что работает — Backend

| Фича | Статус | Примечание |
|------|--------|------------|
| NAT только для client-iface (disableRoutes) | ✅ TESTED | interconnect — без MASQUERADE |
| H1-H4 non-overlapping zones | ✅ TESTED | 4 зоны по 1073741823 uint32 |
| Peer model: peerType, clientAllowedIPs, enabled, PSK | ✅ | |
| addPeer: autoAllocateIP, generateKeys | ✅ | |
| getStatus: transferRx/Tx, latestHandshake, runtimeEndpoint | ✅ | polling через setInterval |
| Export/Import interconnect peer JSON workflow | ✅ TESTED | PSK координация работает |
| AWG kernel deadlock fix: syncconf + restart | ✅ TESTED | awg set peer → убран для AWG2 |
| Util.exec timeout 30s / getStatus 5s | ✅ | SIGKILL при превышении |
| PATCH /api/tunnel-interfaces/:id | ✅ | hot-reload через syncconf, без даунтайма |
| RouteManager: getKernelRoutes (text parse) | ✅ TESTED | ip route show (без -j), работает на всех ядрах |
| RouteManager: getRoutingTables (ip rule show) | ✅ TESTED | обнаруживает хостовые таблицы (table 100 vpn_kz) |
| RouteManager: testRoute (text parse) | ✅ TESTED | ip route get [mark N], без -j |
| FirewallManager: simulateTrace(srcIP, dstIP) | ✅ TESTED | проходит по PBR правилам (fwmark, order), ipset test + CIDR |
| RouteManager: addRoute/deleteRoute/toggleRoute | ✅ | персистентность в routes.json, HTTP 400 с деталью ошибки |
| RouteManager: restoreAll() + reapplyForDevice() | ✅ | маршруты восстанавливаются после рестарта контейнера |
| Routing API: GET /api/routing/table | ✅ | kernel routes по таблице |
| Routing API: GET /api/routing/tables | ✅ | список таблиц |
| Routing API: GET /api/routing/test | ✅ TESTED | policy-aware: src+dst → simulateTrace → PBR или default |
| Routing API: GET/POST/PATCH/DELETE /api/routing/routes | ✅ | static routes CRUD |
| NatManager: addRule/updateRule/deleteRule/toggleRule | ✅ | персистентность в nat-rules.json |
| NatManager: idempotent _applyRule (-C check) | ✅ | дубли не создаются при рестарте контейнера |
| NatManager: sourceAliasId — alias как source | ✅ | host/network → N правил, ipset → --match-set, group → рекурсивно |
| NatManager: getNetworkInterfaces | ✅ | ip -o link show (без -j, text parse) |
| NatManager: eager init в Server constructor | ✅ | правила применяются при старте контейнера |
| NAT API: GET /api/nat/interfaces | ✅ | список интерфейсов хоста |
| NAT API: GET/POST/PATCH/DELETE /api/nat/rules | ✅ | CRUD правил, toggle через PATCH {enabled} |
| NAT API: GET /api/nat/rules → auto rules от интерфейсов | ✅ | badge "auto", read-only |
| AwgParamGenerator: generate() | ✅ | Jc/Jmin/Jmax + S1-S4 + H1-H4 + I1-I5 (7 CPS-профилей) |
| Templates API: POST /api/templates/generate | ✅ | генерация + опциональное сохранение (saveName) |
| GatewayManager: createGateway/updateGateway/deleteGateway | ✅ | персистентность в /etc/wireguard/data/gateways/ |
| GatewayMonitor: ping-polling, latency/loss статистика | ✅ | per-gateway интервал, windowSeconds |
| GatewayMonitor: HTTP(S) probe — native Node.js http/https | ✅ | интервал 10s по умолч., HTTP код в статусе, health rules |
| GatewayMonitor: extends EventEmitter, emit statusChange | ✅ | FirewallManager подписывается на события |
| GatewayGroup: CRUD, tier-based приоритеты | ✅ | trigger: packetloss/latency/packetloss_latency |
| FirewallManager: fallback при gateway down | ✅ | blackhole или default gw, 30s anti-flap, _fallbackActive + _restoreTimers |
| FirewallManager: ip route replace (идемпотентно) | ✅ | вместо ip route add — не падает при stale fallback route |
| AliasManager: CRUD host/network/ipset/group | ✅ | group = merged deduplicated entries; members только host/network |
| AliasManager: port/port-group типы | ✅ | entries: tcp:443, udp:53, any:80, tcp:8080-8090; getPortMatchSpec() |
| IpsetManager: create/destroy/loadFromFile/generateFromScript | ✅ | prefixes.py интеграция |
| FirewallManager: init chains (FIREWALL_FORWARD + FIREWALL_MANGLE) | ✅ | filter + mangle custom chains |
| FirewallManager: CRUD + toggle + move | ✅ | персистентность в firewall-rules.json |
| FirewallManager: _rebuildChains() | ✅ | flush + re-apply all enabled rules in order |
| FirewallManager: PBR (accept + gateway) | ✅ | mangle MARK + ip route table + ip rule + filter ACCEPT |
| FirewallManager: migration от PolicyManager | ✅ | policy-rules.json → firewall-rules.json при первом старте |
| Firewall API: GET/POST/PATCH/DELETE /api/firewall/rules | ✅ | CRUD + toggle |
| Firewall API: POST /api/firewall/rules/:id/move | ✅ | up/down |
| Firewall API: GET /api/firewall/interfaces | ✅ | список интерфейсов хоста |

### ✅ Что работает — Frontend

| Страница/Компонент | Статус | Детали |
|-------------------|--------|--------|
| Sidebar (6 пунктов) | ✅ | Interfaces, Gateways, Routing, Firewall, Settings, Administration |
| Interfaces: вкладка "All" (дашборд) | ✅ | все пиры всех интерфейсов, дефолтный вид |
| Interfaces: dynamic tabs | ✅ | по одной вкладке на интерфейс |
| Interfaces: per-interface view | ✅ | Info card + Peers list |
| Interface card: "Edit" кнопка | ✅ | модал: имя, адрес, порт, AWG2 профиль |
| Interface card: "Export My Params" | ✅ | скачивает JSON для другой стороны |
| Dashboard peer cards | ✅ | серый бейдж interfaceName, все действия работают |
| Peers: create modal (Client/Interconnect toggle) | ✅ | peerType выбирается до создания |
| Peers: "Import JSON" кнопка | ✅ | interconnect workflow |
| Peer cards: S2S badge | ✅ | синий тег для peerType=interconnect |
| Peer cards: runtimeEndpoint | ✅ | IP:port из wg dump (обновляется ~1s) |
| Peer cards: online/offline dot | ✅ | красный мигающий = online |
| Peer cards: RX/TX stats | ✅ | текущий и накопленный трафик |
| Peer cards: enable/disable toggle | ✅ | |
| Peer cards: QR/download (только client) | ✅ | downloadableConfig = !!privateKey |
| Settings: Global Settings + AWG2 Templates | ✅ | |
| AWG2 дропдаун доступен сразу после логина | ✅ | fix: loadSettings() в login() |
| Administration: Admin Tunnel (старый Clients) | ✅ | |
| Routing: Status tab (kernel routes + route test) | ✅ TESTED | policy-aware Route Lookup: src→dst, PBR match + ipset test, протестировано на КЗ |
| Routing: Static tab (CRUD + toggle) | ✅ | персистентность в routes.json |
| Routing: OSPF tab | ⏳ | placeholder "Coming soon" |
| Routing: таблица 100 в дропдауне | ✅ TESTED | обнаруживается через ip rule show |
| Toast-уведомления (правый верхний угол) | ✅ | зелёный (success) / красный (error), 7с, стекируются, dismiss × |
| NAT: Outbound NAT tab (CRUD таблица правил + toggle) | ✅ | |
| NAT: Add/Edit Rule modal — alias source | ✅ | радиокнопка Alias + дропдаун; фиолетовый бейдж в таблице |
| NAT: auto правила от интерфейсов | ✅ | badge "auto" + ссылка на интерфейс; read-only |
| NAT: Port Forwarding tab | ⏳ | placeholder "Coming soon" |
| Gateways: список, create/edit/delete modal | ✅ | name, interface, gatewayIP, monitorAddress, interval |
| Gateways: live статус (online/latency/loss/HTTP) | ✅ | GatewayMonitor ping + HTTP polling |
| Gateways: fallbackToDefault checkbox в Firewall Rule | ✅ | blackhole или default gw при gateway down |
| Gateway Groups: create/edit/delete, tier-based | ✅ | trigger: packetloss/latency/packetloss_latency |
| Firewall → Aliases | ✅ | host/network/ipset/group/port/port-group, upload, generate (RIPE), CRUD |
| Firewall → Aliases: CSS utility classes | ✅ | modal-overlay/modal-panel/modal-header/body/footer/footer-compact + px-2/py-1 |
| Firewall → Rules | ✅ | ACCEPT/DROP/REJECT, PBR gateway + fallback, port alias (source/destination) |
| Routing → Policy tab | ❌ удалён | PBR переехал в Firewall → Rules |
| Settings: "Generate" кнопка (⚡) | ✅ | модал: профиль + intensity + host + preview + save |
| Generate modal: 7 CPS-профилей | ✅ | QUIC Initial/0-RTT, TLS 1.3, DTLS, HTTP/3, SIP, Noise_IK |
| Generate modal: Edit & Save | ✅ | переносит params в templateForm → стандартный template modal |

### ❌ Что не реализовано

1. **Admin Instance backend** — `src/lib/AdminInstance.js` (управление wg0/admin-туннелем через новую архитектуру)
2. **Port Forwarding (DNAT)** — backend + UI (страница NAT, вкладка Port Forwarding)

---

## S2S Interconnect Workflow (реализован, протестирован)

Сценарий: два сервера (A и B), нужно создать S2S туннель.

```
Сервер A:                          Сервер B:
1. Создать интерфейс wg10           1. Создать интерфейс wg10
   (адрес 10.100.0.1/24)               (адрес 10.100.1.1/24)
2. Export My Params →               2. Import JSON (файл от A)
   скачать wg10-params.json            → создаётся Interconnect peer
   { publicKey, endpoint,              → PSK генерируется автоматически
     address, protocol }
                                    3. Export My Params →
                                       скачать wg10-params.json
                                       { publicKey, endpoint,
                                         address, protocol,
                                         presharedKey }  ← PSK включён!
4. Import JSON (файл от B)
   → создаётся Interconnect peer
   → PSK берётся из файла (sync!)
```

**Ключевые детали реализации:**
- `export-params`: возвращает publicKey + endpoint (WG_HOST:port) + address + protocol
  + presharedKey если уже есть interconnect peer с PSK
- `import-json`: принимает оба формата — interface-params (address → /32) и peer-params (allowedIPs напрямую)
- AllowedIPs для interconnect пира = `<remote_ip>/32` (не подсеть — крипто-роутинг только до пира)
- PSK: сторона B генерирует при первом импорте, потом включает в свой export → A получает при импорте

---

## Следующие задачи (по приоритету)

### 1. Admin Instance (приоритет: средний)
**Что нужно:**
- `src/lib/AdminInstance.js` — загрузка из ENV vars (WG_DEFAULT_ADDRESS, WG_DEFAULT_DNS, etc.)
- Страница Administration: показать статус admin-туннеля, список клиентов (из WireGuard.js)
- **Файлы:** новый `src/lib/AdminInstance.js`, `src/www/index.html` (страница Administration), `src/lib/Server.js` (API)

### 2. Port Forwarding / DNAT (приоритет: средний)
- Backend: `NatManager.addDnatRule()` — `iptables-nft -t nat -A PREROUTING -p tcp --dport PORT -j DNAT --to DEST`
- UI: страница NAT, вкладка Port Forwarding (сейчас placeholder "Coming soon")

### 3. UI Config через API (приоритет: низкий)
**Что сейчас:** Эндпоинты `/api/ui-traffic-stats`, `/api/ui-chart-type`, `/api/lang`,
`/api/wg-enable-one-time-links`, `/api/ui-sort-clients`, `/api/wg-enable-expire-time`,
`/api/ui-avatar-settings`, `/api/remember-me` — **только GET**, читают переменные окружения
из `src/config`. Изменить без рестарта контейнера нельзя.

**Что сделать:**
- Перенести эти параметры из env-переменных в `settings.json`
- Добавить `PUT /api/ui-config` (или расширить `PUT /api/settings`) для записи
- UI: раздел настроек интерфейса в Settings без необходимости редактировать `docker-compose.yml`

**Файлы:** `src/config.js`, `src/lib/Settings.js`, `src/lib/Server.js`, `src/www/index.html`

Полный список → `REQUIREMENTS.md` раздел "🚧 Не реализовано".

---

## Детали реализации — Dashboard + Edit Interface

### Dashboard (вкладка "All")

**Данные:**
- `allPeers: []` — реактивный массив, каждый peer имеет `peer.interfaceId` + `peer.interfaceName`
- `_peerIfaceId(peer)` — возвращает `peer.interfaceId || activeInterfaceId` — правильный iface для API-вызовов
- `_refreshPeersOrAll()` — после действий над пиром вызывает нужный refresh (per-iface или all)

**Polling:**
```javascript
setInterval(() => {
  if (activePage === 'interfaces') {
    if (activeInterfaceId) refreshPeers();
    else refreshAllPeers(); // dashboard mode
  }
}, 1000);
```

**Watcher `activeInterfaceId`:** при переключении на null → немедленно вызывает `refreshAllPeers()`.

**Startup:** `loadTunnelInterfaces().then(() => refreshAllPeers())` — дашборд заполняется сразу.

### Edit Interface Modal

**Данные:** `showInterfaceEdit: false`, `interfaceEdit: { id, name, address, listenPort, disableRoutes, protocol, selectedTemplateId, settings: { jc, jmin, jmax, s1-s4, h1-h4, i1-i5 } }`

**Методы:**
- `openInterfaceEdit(iface)` — заполняет interfaceEdit из iface.data + открывает модал
- `onEditInterfaceTemplateSelect(templateId)` — заполняет interfaceEdit.settings из шаблона
- `saveInterfaceEdit()` — валидация → `api.updateTunnelInterface()` → `_applyInterfaceUpdate()` (Vue splice)

**Кнопка:** фиолетовая outline "Edit" в ряду кнопок interface info card.

**Backend flow:** `PATCH /api/tunnel-interfaces/:id` → `Object.assign(data, updates)` → `save()` → `regenerateConfig()` → `reload()` (syncconf, без даунтайма).

### Fix: AWG2 дропдаун после логина

**Проблема:** `loadSettings()` в mounted() получал 401, если приложение требует пароль. После логина вызывался только `loadTunnelInterfaces()`, шаблоны не загружались.

**Фикс:** добавлен `this.loadSettings()` рядом с `this.loadTunnelInterfaces()` в `login()` handler.
