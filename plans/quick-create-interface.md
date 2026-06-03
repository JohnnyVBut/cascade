# Plan: Quick/Manual Interface Creation

**Date:** 2026-03-28
**Status:** Design finalized, ready for implementation
**Estimate:** ~7.5 hours, 5 files

---

## Финальная спецификация (согласовано)

### Quick mode

- **Назначение:** только client-интерфейсы (`disableRoutes=false`)
- **Поля в UI:** Name (опционально, дефолт = ID интерфейса), Protocol
- **Адрес:** автоматически — первый доступный /24 из `subnetPool`, первый хост (/24)
  - Пример: `192.168.12.1/24`
- **Порт:** автоматически — первый доступный из `portPool`
  - Проверка: `net.Listen("udp", ":PORT")` bind test + проверка существующих интерфейсов
- **AWG2 параметры:** использовать дефолтный шаблон если задан, иначе random профиль
- **Запуск:** сразу после создания (`wg-quick up`)
- **Toast (успех):**
  ```
  ✅ wg12 created & started
     192.168.12.1/24 · UDP 51833 · AWG2
  ```
- **Toast (создан, но не запустился):**
  ```
  ⚠️ wg12 created but failed to start
     Port 51833 is already in use
     [Open Interface →]
  ```
- **Pool exhausted:** error toast `"No available ports/subnets in pool"`

### Manual mode

- Полная существующая форма без изменений
- **Новое:** кнопка `Generate ⚡` рядом с дропдауном шаблонов в AWG2 секции
  - Генерирует random профиль, заполняет поля формы
  - Без выбора CPS-профиля (random, потом можно отредактировать)
- **Порт:** prefill = следующий из portPool, свободно редактируемый
- **IX интерфейсы:** disableRoutes toggle виден → Quick таб недоступен автоматически

---

## Settings — новые поля

```json
{
  "subnetPool": "192.168.0.0/16",
  "portPool": "51831-65535"
}
```

### portPool формат
- Диапазон: `51831-65535`
- Явные порты: `51831, 51832, 51835`
- Смешанный: `51831-51840, 52000, 54321-54330`
- Пробелы вокруг разделителей игнорируются
- Валидация при сохранении: порты 1024–65535, корректный формат
- При исчерпании пула: error toast

### subnetPool формат
- Валидный CIDR: `192.168.0.0/16`
- Валидация при сохранении

### Settings UI
Добавить в секцию Global Settings:
```
Subnet Pool:  [ 192.168.0.0/16  ]
Port Pool:    [ 51831-65535      ]
```

---

## Файлы для изменения

| Файл | Изменение |
|------|-----------|
| `internal/settings/settings.go` | Добавить `SubnetPool string`, `PortPool string` в `GlobalSettings`. Default + валидация в `applySettingKey()` |
| `internal/tunnel/manager.go` | Добавить `nextSubnet(pool string)`, улучшить `nextListenPort()` (парсинг portPool + UDP bind test). Новый `QuickCreate()` метод |
| `internal/api/interfaces.go` | Новый handler `POST /api/tunnel-interfaces/quick-create` (регистрировать ДО `/:id`!) |
| `internal/frontend/www/js/app.js` | Добавить `createMode: 'quick'`, `quickCreateTunnelInterface()`, `generateAndFillInterfaceParams()` |
| `internal/frontend/www/index.html` | Pill-toggle Quick/Manual в modal. Quick form (Name + Protocol). Кнопка Generate⚡ в Manual AWG2 секции |

## Файлы НЕ трогать

- `internal/tunnel/interface.go` — FIX-1..FIX-10
- `internal/awgparams/generator.go` — уже полный
- `internal/db/db.go` — миграция не нужна (settings хранятся в key-value таблице)

---

## Детальный план реализации

### Step 1 — Settings: SubnetPool + PortPool (Small, ~1h)

**`internal/settings/settings.go`:**

```go
type GlobalSettings struct {
    // ... existing fields ...
    SubnetPool string `json:"subnetPool"` // default "192.168.0.0/16"
    PortPool   string `json:"portPool"`   // default "51831-65535"
}
```

В `applySettingKey()`:
- `"subnetPool"`: валидировать через `net.ParseCIDR()`, отклонять невалидные
- `"portPool"`: парсить через `parsePortPool()`, отклонять при ошибке

Новая функция `parsePortPool(s string) ([]int, error)`:
- Разбивает по запятой
- Для каждого элемента: range (`A-B`) или одиночный порт
- Возвращает отсортированный список уникальных портов
- Validates 1024 ≤ port ≤ 65535

### Step 2 — Manager: nextSubnet() + улучшенный nextListenPort() (Medium, ~1.5h)

**`internal/tunnel/manager.go`:**

```go
// nextSubnet parses pool CIDR, finds first /24 not used by existing interfaces.
// Returns "X.X.X.1/24" (first host in the /24).
func (m *Manager) nextSubnet(pool string) (string, error)

// nextListenPort теперь принимает portPool string (из settings).
// Парсит пул, итерирует, проверяет:
//   1. Не занят существующими интерфейсами
//   2. UDP bind test: net.ListenPacket("udp", ":PORT")
func (m *Manager) nextListenPort(portPool string) (int, error)

// QuickCreate создаёт и запускает client-интерфейс одной операцией.
// Возвращает созданный интерфейс + ошибку запуска (если была).
func (m *Manager) QuickCreate(name, protocol string) (*TunnelInterface, error, error)
// (createErr, startErr — разные категории ошибок)
```

`QuickCreate()` flow:
1. `settings.GetSettings()` → получить subnetPool, portPool, defaultTemplate
2. `nextSubnet(subnetPool)` → address
3. `nextListenPort(portPool)` → port
4. Если AWG2: `settings.ApplyDefaultTemplate()` или `awgparams.Generate(Options{Profile:"random"})`
5. `CreateInterface(...)` → iface
6. `iface.Start()` → startErr (не фатальный, возвращается отдельно)
7. Return iface, nil, startErr

### Step 3 — API: POST /quick-create (Small, ~0.5h)

**`internal/api/interfaces.go`:**

```go
// POST /api/tunnel-interfaces/quick-create
// Body: { name?: string, protocol?: string }
// Response: { interface: {...}, started: bool, startError?: string }
func quickCreateInterface(c *fiber.Ctx) error {
    var body struct {
        Name     string `json:"name"`
        Protocol string `json:"protocol"`
    }
    // ...
    iface, createErr, startErr := tunnel.Get().QuickCreate(body.Name, body.Protocol)
    if createErr != nil {
        return fiber.NewError(400, createErr.Error())
    }
    resp := fiber.Map{
        "interface": iface.ToJSON(),
        "started":   startErr == nil,
    }
    if startErr != nil {
        resp["startError"] = startErr.Error()
    }
    return c.JSON(resp)
}
```

**Регистрация — ОБЯЗАТЕЛЬНО до `/:id`:**
```go
api.Post("/quick-create", quickCreateInterface)  // BEFORE /:id
api.Get("/:id", getInterface)
// ...
```

### Step 4 — Frontend: app.js (Medium, ~1.5h)

```javascript
// data()
createMode: 'quick',   // 'quick' | 'manual'

// Методы:
async quickCreateTunnelInterface() {
    const res = await this.api.call({
        method: 'POST',
        path: '/tunnel-interfaces/quick-create',
        body: {
            name: this.interfaceCreate.name || undefined,
            protocol: this.interfaceCreate.protocol,
        }
    })
    this.showInterfaceCreate = false
    this.resetInterfaceCreate()
    await this.loadTunnelInterfaces()

    const iface = res.interface
    const addr = iface.data?.address || ''
    const port = iface.data?.listenPort || ''
    const proto = iface.data?.protocol === 'amneziawg-2.0' ? ' · AWG2' : ''

    if (res.started) {
        this.showToast(`${iface.id} created & started\n${addr} · UDP ${port}${proto}`, 'success')
    } else {
        this.showToast(
            `${iface.id} created but failed to start\n${res.startError || 'Unknown error'}`,
            'warning',
            { action: { label: 'Open Interface', fn: () => this.openInterface(iface.id) } }
        )
    }
},

async generateAndFillInterfaceParams() {
    const res = await this.api.call({
        method: 'POST',
        path: '/templates/generate',
        body: { profile: 'random', intensity: 'medium' }
    })
    // Заполнить interfaceCreate.settings из res
    Object.assign(this.interfaceCreate.settings, res)
},
```

### Step 5 — Frontend: index.html (Medium, ~2h)

Pill-toggle в верхней части тела модала:
```html
<div class="flex rounded overflow-hidden border dark:border-neutral-500" style="width:fit-content;">
  <button @click="createMode='quick'"
    :class="createMode==='quick' ? 'bg-purple-600 text-white' : 'text-gray-600 dark:text-neutral-300'"
    style="padding:4px 16px;" class="text-sm font-medium transition">
    Quick
  </button>
  <button @click="createMode='manual'"
    :class="createMode==='manual' ? 'bg-purple-600 text-white' : 'text-gray-600 dark:text-neutral-300'"
    style="padding:4px 16px;" class="text-sm font-medium transition">
    Manual
  </button>
</div>
```

Quick form (под toggle):
```html
<div v-if="createMode==='quick'">
  <!-- Name (optional) -->
  <!-- Protocol selector -->
  <!-- Note: "Address and port will be assigned automatically" -->
</div>
```

Manual form:
```html
<div v-if="createMode==='manual'">
  <!-- Существующая форма полностью -->
  <!-- В AWG2 секции добавить Generate⚡ кнопку: -->
  <button @click="generateAndFillInterfaceParams()" ...>Generate ⚡</button>
</div>
```

Footer: кнопка Create вызывает нужный метод:
```html
<button @click="createMode==='quick' ? quickCreateTunnelInterface() : createTunnelInterface()">
  Create
</button>
```

---

## Тесты (обязательно)

| Тест | Файл |
|------|------|
| `TestNextSubnet_FirstAvailable` | `manager_test.go` |
| `TestNextSubnet_SkipsUsed` | `manager_test.go` |
| `TestNextSubnet_PoolExhausted` | `manager_test.go` |
| `TestParsePortPool_Range` | `settings_test.go` |
| `TestParsePortPool_Mixed` | `settings_test.go` |
| `TestParsePortPool_Invalid` | `settings_test.go` |
| `TestSettings_PortPool_Default` | `settings_test.go` |
| `TestSettings_SubnetPool_Default` | `settings_test.go` |

---

## Порядок коммитов

```
1. feat(settings): add SubnetPool + PortPool with validation
2. feat(tunnel): nextSubnet() + improved nextListenPort() + QuickCreate()
3. feat(api): POST /tunnel-interfaces/quick-create
4. feat(ui): Quick/Manual mode toggle + Generate button in interface create modal
```

---

## Риски

| Риск | Митигация |
|------|-----------|
| Route conflict `/quick-create` vs `/:id` | Регистрировать ДО `/:id` (как `/templates/generate`) |
| `net.Listen` bind test занимает время при большом пуле | Timeout 100ms на попытку |
| `createMode` не сбрасывается при закрытии модала | Явный reset в `resetInterfaceCreate()` |
| AWG2 Quick + нет шаблона + генератор падает | Обернуть в try/catch, fallback на пустые AWG2 params |
