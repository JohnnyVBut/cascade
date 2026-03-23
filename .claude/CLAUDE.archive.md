# Cascade — Archive (Legacy Node.js + old context)
# Этот файл содержит устаревший контекст из Node.js версии (feature/kernel-module).
# Хранится на случай если понадобится вернуться к старому коду.
# Активный проект: Go rewrite (feature/go-rewrite) — см. CLAUDE.md

---

## ПРАВИЛО №0 (Node.js): ДОКУМЕНТАЦИЯ API

При добавлении, изменении или удалении любого API endpoint — обновить оба файла:
- `docs/API.md` — русская версия
- `docs/API.en.md` — английская версия
Коммитить вместе с изменениями в `Server.js`.

---

## КРИТИЧЕСКИЕ ФИКСЫ Node.js (FIX-1 — FIX-15)

### FIX-1: iptables-nft + FORWARD в обоих направлениях
Файл: `src/lib/TunnelInterface.js` → generateWgConfig()
PostUp использует `-A FORWARD` (append), НЕ `-I FORWARD` (insert).
FORWARD нужен -i И -o. NAT только если disableRoutes=false.

### FIX-2: Конфиг регенерируется перед start() + down→up при "already exists"
Файл: `src/lib/TunnelInterface.js` → start()
`--network host` → интерфейс переживает docker restart → "already exists".
Всегда: await this.regenerateConfig() перед подъёмом.

### FIX-3: stop() игнорирует "not a WireGuard/AmneziaWG interface"
Файл: `src/lib/TunnelInterface.js` → stop()
Интерфейс уже остановлен — нормально, не крашить.

### FIX-4: Non-overlapping H1-H4 (4 зоны uint32)
Файл: `src/lib/Settings.js` → generateRandomHRanges()
H1-H4 не должны пересекаться. Uint32 делится на 4 равные зоны.

### FIX-5: Vue 2 — обновление массива только через splice
Файл: `src/www/js/app.js` → _applyInterfaceUpdate()
array[idx] = newItem не триггерит реактивность Vue 2. Только splice.

### FIX-6: Address пира из AllowedIPs + маска интерфейса
Файл: `src/lib/Peer.js` → _generateCompleteConfig()
Поле remoteAddress удалено. Address = IP из AllowedIPs + маска iface.

### FIX-7: _quickBin / _syncBin по протоколу
Файл: `src/lib/TunnelInterface.js`
amneziawg-2.0 → awg-quick/awg, wireguard-1.0 → wg-quick/wg.

### FIX-8: _kernelRemovePeer — AWG2 использует restart()
Файл: `src/lib/TunnelInterface.js`
awg set peer remove дедлочится. Использовать restart(). Сериализовать через _reloadMutex.

### FIX-9: _kernelSetPeer — AWG2 использует syncconf
Файл: `src/lib/TunnelInterface.js`
awg set peer нестабилен (10-15s, дедлок). Для AWG2: reload() (awg syncconf).

### FIX-10: Util.exec timeout 30s по умолчанию
Файл: `src/lib/Util.js`
Без timeout зависший awg/wg процесс живёт вечно → сотни зомби → container freeze.

### FIX-11: ip -j ЗАПРЕЩЕНО — зависает на некоторых ядрах Linux
Файл: `src/lib/RouteManager.js`
ip -j route show зависает навсегда на production (Москва). Только текстовый вывод + парсинг.

### FIX-12: HTTP method в fetch() — ВСЕГДА uppercase
Файл: `src/www/js/api.js`
Node.js 22 llhttp: PATCH lowercase → ERR_CONNECTION_RESET. method.toUpperCase().

### FIX-13: Порядок инициализации — InterfaceManager ПЕРВЫМ
Файл: `src/lib/Server.js`
RouteManager параллельно с InterfaceManager → маршруты добавлялись до интерфейса.
Chain: InterfaceManager → RouteManager.restoreAll() → NatManager.init()

### FIX-14: NAT rules идемпотентность через iptables -C
Файл: `src/lib/NatManager.js`
--network host → iptables сохраняются при рестарте. Проверять -C перед -A.

### FIX-15: ip route errors → HTTP 400 с деталью из stderr
Файл: `src/lib/RouteManager.js`
Без обёртки h3 возвращал 500. Ловить err.stderr → createError({ status: 400 }).

### FIX-15b: Gateway fallback — blackhole/default route при gateway down
Файлы: `src/lib/GatewayMonitor.js`, `src/lib/FirewallManager.js`
GatewayMonitor extends EventEmitter → emit statusChange → FirewallManager подписывается.
fallbackToDefault: true → default gw, false → blackhole. Anti-flap 30s.

---

## Архитектура Node.js (feature/kernel-module)

**Стек:** Node.js, h3, bcryptjs, QRCode, express-session, Vue 2, Tailwind CSS CDN

**Хранилище:** `/etc/wireguard/data/` — JSON файлы (settings.json, interfaces/*.json, peers/**/*.json)

**Ключевые файлы:**
- `src/lib/Server.js` — HTTP сервер (h3), все API маршруты
- `src/lib/Settings.js` — Singleton: global settings + AWG2 templates
- `src/lib/InterfaceManager.js` — управление интерфейсами
- `src/lib/TunnelInterface.js` — один WG/AWG интерфейс
- `src/lib/Peer.js` — модель пира
- `src/www/js/api.js` — клиентские API методы
- `src/www/js/app.js` — Vue app
- `src/www/index.html` — весь UI

---

## Checkpoint Node.js (feature/kernel-module)

**Последний коммит:** `586a515` fix(routing): simulateTrace uses FirewallManager

**Что готово:**
- Interfaces: CRUD, start/stop, peers, S2S interconnect, dashboard
- Routing: static routes + status + policy-aware Route Lookup
- NAT: Outbound MASQUERADE/SNAT CRUD + alias source + auto правила
- Gateways: CRUD + live ping/HTTP monitoring + Gateway Groups + fallback
- Firewall Aliases: host/network/ipset/group + L4 port/port-group + upload + generate
- Firewall Rules: ACCEPT/DROP/REJECT + PBR (gateway) + port alias matching + ↑↓ order
- AWG2 Templates: CRUD + Generate (7 CPS-профилей)

**Не реализовано:**
- Admin Instance backend (src/lib/AdminInstance.js)
- Port Forwarding (DNAT)

---

## Коммиты feature/kernel-module (62 коммита)

de31c42 fix: iptables-nft + FORWARD ACCEPT в PostUp/PostDown
8892179 fix: регенерация конфига + down→up при "already exists"
359984e fix: H1-H4 как non-overlapping ranges
63a7a18 refactor: убран remoteAddress, Address вычисляется из AllowedIPs
c83b983 feat: Settings.js + Settings/Templates API
7482fc2 feat(ui): вкладка Settings (Global Settings + AWG2 Templates)
aa4feda fix(ui): reactive status update after start/stop/restart
f8ca1ab feat(ui): S2S badge + runtimeEndpoint в карточке пира
a3d0aa5 fix: interconnect peer allowedIPs = host /32, не подсеть
028a7c5 fix: mutex для _kernelSetPeer + exec timeout
b3c53be fix: AWG2 _kernelSetPeer → awg syncconf вместо awg set peer
337869e feat(ui): dashboard view
943a046 feat(ui): Edit Interface modal
075ebc8 fix(ui): загрузка templates после логина
138e33d feat: Routing page
152f010 fix: routing table discovery via kernel (ip rule show)
2f025c3 fix: add iproute2 explicitly to Dockerfile
36b3191 fix(ui): show kernelRoutesError + loading indicator
ef5d5cc fix(ui): parallel loading + kernelRoutesLoading state
b3c7153 fix: remove ALL ip -j usage
dfc34c8 fix: toJSON() missing settings + api.js json error
1579be6 fix: uppercase HTTP methods in api.js (Node.js 22 llhttp)
d75d7d5 feat(ui): toast notification system
a0a41bf feat: NAT page
09df40e fix: RouteManager eager init
8f53a48 fix: static routes persist after restart
97d64ff fix: routes restored correctly after restart (FIX-13 v3)
f085bfa fix(routing): ip route kernel errors → HTTP 400
d40d56b fix: NAT rule deduplication via iptables -C
47e91bf fix: NAT rules deduplicated on container restart
25948d6 chore: prefixes.py
1b54fd8 feat: AWG2 parameter generator
fc0df23 fix(generator): remove <c> tag
3be4389 feat: Firewall Aliases + Policy-Based Routing
18930a3 fix(ipset): pure Node.js PrefixFetcher
0b59bb2 fix(ui): alias auto-generate on create
f90eea5 fix(ui): alias table
ab1b9cb fix(api): alias/policy CRUD
faf13f4 refactor(ui): Upload button in Edit modal
2025867 feat(ipset): CIDR aggregation
57a9352 fix(ui): alias edit modal
f639ffa fix(ui): restore public IP display
edec89a docs: add English API reference
b566fce docs: require API docs update
1bdbd8f docs: wishlist item
c488cca feat: Firewall Rules
343fabf feat(gateways): HTTP(S) monitoring
7a6b225 fix(gateways): HTTP probe starts by monitorRule
4d8a693 fix(gateways): HTTP window auto-expands
ea90153 fix(docker): add curl to apk
6257d80 fix(gateway-monitor): native Node.js http/https probes
f6bac16 feat(gateway-monitor): HTTP response code in status
17188eb fix(gateway-monitor): reduce default interval to 10s
bfebc98 feat: gateway fallback
bb2742e feat(aliases): group type
fb8f83a feat: L4 port aliases
85cc147 refactor(ui): CSS utility classes
96ed64f fix(ui): port mode button None → Any
f1e02ac fix(ui): add missing px-2/py-1
37f18a8 feat(nat): auto MASQUERADE rules from interfaces
00b9fc0 fix(nat): use getAllInterfaces()
c722be5 feat(nat): alias support in NAT rules
586a515 fix(routing): simulateTrace uses FirewallManager
