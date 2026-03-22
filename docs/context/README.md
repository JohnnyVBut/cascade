# 🚀 Новая архитектура: Tunnel Interfaces + Peers

## 📁 Созданные файлы:

### Backend классы (src/lib/):

1. **TunnelInterface.js** (10 KB)
   - Класс для управления туннельным интерфейсом (wg10, wg11, etc.)
   - Один интерфейс → много peers (1:N модель)
   - Генерирует WireGuard конфиг из интерфейса + всех peers
   - Методы: start(), stop(), restart(), reload()

2. **Peer.js** (11 KB)
   - Класс для представления удалённого подключения
   - Содержит: publicKey, endpoint, allowedIPs, remoteAddress
   - Валидация данных peer
   - Генерация remote конфига для скачивания

3. **InterfaceManager.js** (8.7 KB)
   - Singleton менеджер всех интерфейсов
   - Управление созданием/удалением интерфейсов
   - Auto-assign портов и имён интерфейсов
   - Управление peers через интерфейсы

### API Routes (src/routes/):

4. **tunnel-interfaces.js** (11 KB)
   - Express routes для REST API
   - Endpoints для интерфейсов и peers
   - Полный CRUD для обеих сущностей

---

## 🏗️ Архитектура:

```
┌─────────────────────────────────────────────────────┐
│                InterfaceManager                     │
│                   (singleton)                       │
├─────────────────────────────────────────────────────┤
│  - Управление всеми интерфейсами                   │
│  - Auto-assign портов и имён                       │
│  - Singleton instance                              │
└──────────────┬──────────────────────────────────────┘
               │
               ├─→ TunnelInterface (wg10)
               │     ├─→ Peer "Office-A"
               │     ├─→ Peer "Office-B"
               │     └─→ Peer "Office-C"
               │
               ├─→ TunnelInterface (wg11)
               │     └─→ Peer "AWS-DC"
               │
               └─→ TunnelInterface (wg12)
                     ├─→ Peer "Branch-1"
                     └─→ Peer "Branch-2"
```

---

## 📂 Файловая структура:

```
/etc/wireguard/
├── data/
│   ├── interfaces/
│   │   ├── wg10.json          ← Данные интерфейса
│   │   ├── wg11.json
│   │   └── wg12.json
│   └── peers/
│       ├── wg10/
│       │   ├── uuid-1.json    ← Данные peer
│       │   ├── uuid-2.json
│       │   └── uuid-3.json
│       ├── wg11/
│       │   └── uuid-4.json
│       └── wg12/
│           ├── uuid-5.json
│           └── uuid-6.json
│
├── wg10.conf                  ← Генерируется из wg10.json + peers
├── wg11.conf
└── wg12.conf
```

---

## 🔌 API Endpoints:

### Interfaces:

```
GET    /api/tunnel-interfaces              - Список интерфейсов
POST   /api/tunnel-interfaces              - Создать интерфейс
GET    /api/tunnel-interfaces/:id          - Инфо об интерфейсе
PATCH  /api/tunnel-interfaces/:id          - Обновить интерфейс
DELETE /api/tunnel-interfaces/:id          - Удалить интерфейс
POST   /api/tunnel-interfaces/:id/start    - Запустить
POST   /api/tunnel-interfaces/:id/stop     - Остановить
POST   /api/tunnel-interfaces/:id/restart  - Перезапустить
```

### Peers:

```
GET    /api/tunnel-interfaces/:id/peers                - Список peers
POST   /api/tunnel-interfaces/:id/peers                - Добавить peer
GET    /api/tunnel-interfaces/:id/peers/:peerId        - Инфо о peer
PATCH  /api/tunnel-interfaces/:id/peers/:peerId        - Обновить peer
DELETE /api/tunnel-interfaces/:id/peers/:peerId        - Удалить peer
GET    /api/tunnel-interfaces/:id/peers/:peerId/config - Скачать конфиг
```

---

## 📋 Примеры использования API:

### 1. Создать интерфейс:

```bash
POST /api/tunnel-interfaces
{
  "name": "Main VPN Hub",
  "protocol": "wireguard-1.0",
  "address": "10.100.0.1/24",
  "listenPort": 51830
}

Response:
{
  "interface": {
    "id": "wg10",
    "name": "Main VPN Hub",
    "protocol": "wireguard-1.0",
    "listenPort": 51830,
    "address": "10.100.0.1/24",
    "publicKey": "...",
    "enabled": false,
    "peerCount": 0
  }
}
```

### 2. Добавить peer:

```bash
POST /api/tunnel-interfaces/wg10/peers
{
  "name": "Office-A",
  "publicKey": "aBcD1234...",
  "endpoint": "office-a.com:51820",
  "allowedIPs": "192.168.1.0/24",
  "remoteAddress": "10.100.0.2/24",
  "persistentKeepalive": 25
}

Response:
{
  "peer": {
    "id": "uuid-1234",
    "name": "Office-A",
    "interfaceId": "wg10",
    "publicKey": "aBcD1234...",
    "endpoint": "office-a.com:51820",
    "allowedIPs": "192.168.1.0/24",
    "remoteAddress": "10.100.0.2/24",
    "enabled": true
  }
}
```

### 3. Скачать конфиг для peer:

```bash
GET /api/tunnel-interfaces/wg10/peers/uuid-1234/config

Response: (file download)
# ═══════════════════════════════════════════════════════════════
# Remote Configuration for: Office-A
# Connect to: Main VPN Hub
# Protocol: WireGuard 1.0
# ═══════════════════════════════════════════════════════════════

[Interface]
PrivateKey = YOUR_PRIVATE_KEY
ListenPort = 51820
Address = 10.100.0.2/24

[Peer]
PublicKey = <hub-public-key>
AllowedIPs = 10.100.0.1/24
Endpoint = your-server.com:51830
PersistentKeepalive = 25
```

### 4. Запустить интерфейс:

```bash
POST /api/tunnel-interfaces/wg10/start

Response:
{
  "interface": {
    "id": "wg10",
    "enabled": true,
    ...
  }
}
```

---

## 🎯 Преимущества новой архитектуры:

✅ **Один интерфейс → много peers** (hub-and-spoke)  
✅ **Логическое разделение** (Interface vs Peer)  
✅ **Гибкость** (добавляй/удаляй peers без пересоздания интерфейса)  
✅ **Как в pfSense/OPNsense**  
✅ **Hot reload** (изменения без остановки туннеля)  
✅ **Валидация** (проверка данных перед сохранением)  
✅ **Автоматическая генерация** (ключи, порты, конфиги)

---

## 🔄 Следующие шаги:

1. **Интеграция в Server.js**
   - Добавить routes в Express app
   - Инициализировать InterfaceManager

2. **Frontend UI**
   - Вкладка "Tunnel Interfaces"
   - Формы создания интерфейса/peer
   - Управление peers внутри интерфейса

3. **Тестирование**
   - Unit тесты для классов
   - Integration тесты для API
   - E2E тесты UI

---

**Дата:** 30 января 2026  
**Статус:** ✅ Backend готов к интеграции
