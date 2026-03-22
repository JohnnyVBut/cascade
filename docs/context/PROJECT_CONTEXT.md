# AWG-Easy: Контекст разработки

**Последнее обновление:** 2026-03-01
**Репозиторий:** https://github.com/JohnnyVBut/cascade
**Рабочая ветка:** `feature/wan-tunnels`

---

## Стек технологий

- **Backend:** Node.js, h3 (не Express!), роуты через `createRouter()` + `defineEventHandler()`
- **Frontend:** Vue.js 2 (vanilla, без сборки), Tailwind CSS
- **VPN:** WireGuard + AmneziaWG 2.0
- **Деплой:** Docker, удалённый сервер по SSH

---

## Как работать с проектом

### Редактирование файлов
Всегда редактировать в основной папке:
```
/Users/jenya/PycharmProjects/awg-easy/
```
НЕ в `.claude/worktrees/lucid-lumiere/` — там PyCharm не видит изменений.

### Деплой на сервер
```bash
# Локально — закоммитить и запушить:
git add .
git commit -m "описание изменений"
git push origin feature/wan-tunnels

# На сервере по SSH:
cd awg-easy
git pull origin feature/wan-tunnels
docker build -t awg2-easy:latest .
docker stop awg-easy && docker rm awg-easy
./run.sh
```

### Просмотр логов на сервере
```bash
docker logs -f awg-easy
```

---

## Архитектура

### Старая модель WAN Tunnels (устаревшая, оставлена)
```
WAN Tunnel = Interface + Peer (слитно, 1:1)
wg10 → Office-A
wg11 → Office-B
```
Файлы: `WanTunnel.js`, `TunnelManager.js`
API: `/api/wireguard/wan-tunnels`

### Новая модель Tunnel Interfaces (текущая разработка)
```
Interface wg10 (Hub)
  ├─ Peer "Office-A"
  ├─ Peer "Office-B"
  └─ Peer "Office-C"
```
Файлы: `TunnelInterface.js`, `Peer.js`, `InterfaceManager.js`
API: `/api/tunnel-interfaces`

### Структура данных на сервере
```
/etc/wireguard/
├── wg0.conf                        ← основной WireGuard (клиенты)
├── wg10.conf                       ← конфиг интерфейса wg10 (auto-generated)
├── data/
│   ├── interfaces/
│   │   └── wg10.json              ← данные интерфейса
│   └── peers/
│       └── wg10/
│           └── <uuid>.json        ← данные peer
└── tunnels.json                   ← старые WAN туннели
```

---

## Ключевые файлы

| Файл | Назначение |
|------|-----------|
| `src/lib/Server.js` | Все API роуты (h3), инициализация сервера |
| `src/lib/TunnelInterface.js` | Класс туннельного интерфейса |
| `src/lib/Peer.js` | Класс peer (удалённого подключения) |
| `src/lib/InterfaceManager.js` | Singleton-менеджер интерфейсов |
| `src/lib/Util.js` | Утилиты: `Util.exec()`, `Util.isValidIPv4()` |
| `src/lib/TunnelManager.js` | Старые WAN туннели (deprecated) |
| `src/lib/WanTunnel.js` | Старый класс WAN туннеля (deprecated) |
| `src/www/js/app.js` | Vue.js frontend (~980 строк) |
| `src/www/index.html` | HTML шаблон (~1420 строк) |

---

## Состояние на 2026-03-01

### ✅ Готово и работает:
- VPN Users — управление клиентами WireGuard/AmneziaWG
- WAN Tunnels (старая архитектура) — Site-to-Site 1:1
- Tunnel Interfaces UI — вкладка с двумя под-вкладками (Interfaces / Peers)
- API `/api/tunnel-interfaces` — полный CRUD для интерфейсов и peers
- Создание интерфейса (wg10, wg11...) с авто-назначением портов (51830+)

### 🐛 Исправлено:
- `TunnelInterface.js` — отсутствовал `const Util = require('./Util')` — исправлено 2026-03-01
- `TunnelInterface.js` — AWG 2.0 интерфейсы не запускались (amneziawg-go выходил с "kernel first class support") — исправлено добавлением `WG_PROCESS_FOREGROUND=1` — подтверждено на сервере 2026-03-01
- `TunnelInterface.js` — `start()` падал при двойном вызове ("already exists") — сделан идемпотентным

### ✅ Проверено на сервере (2026-03-01):
- Start AWG 2.0 интерфейса wg10 — работает
- Start wireguard-1.0 интерфейса — работает

### 🔧 Тестируется:
- Stop/Restart интерфейса
- Создание/удаление peer
- Download peer config

### ❌ Не сделано:
- Миграция старых WAN Tunnels → новая архитектура
- Редактирование интерфейса (PATCH)
- Enable/Disable отдельного peer
- Unit/integration тесты

---

## Нумерация интерфейсов и портов

- Интерфейсы: `wg10`, `wg11`, `wg12`, ... (основной wg0 не трогаем)
- Порты: `51830`, `51831`, `51832`, ...
- Порт `51820` — основной WireGuard (клиенты)
- Порт `51821` — Web UI

---

## Важные замечания

### h3 vs Express
Проект использует **h3**, не Express. Это важно при добавлении роутов:
```javascript
// Правильно (h3):
router.get('/api/something', defineEventHandler(async (event) => {
  const id = getRouterParam(event, 'id');
  const body = await readBody(event);
  return { result: '...' };
}))
```

### Util.exec()
Все системные команды через `Util.exec()`:
```javascript
const Util = require('./Util');
await Util.exec('wg genkey');
```
На macOS возвращает пустую строку без выполнения (только Linux).

### InterfaceManager — Singleton
```javascript
const { getInstance } = require('./InterfaceManager');
const manager = await getInstance();
```
