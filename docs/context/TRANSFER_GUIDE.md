# 📤 Инструкция: Передача контекста в новое приложение Claude

> ⚠️ **OBSOLETE — Node.js era context document**
>
> Этот файл описывает передачу контекста для файлов из [PROJECT_CONTEXT.md](PROJECT_CONTEXT.md)
> (Node.js-прототип, `TunnelInterface.js`/`InterfaceManager.js`/`Peer.js`), которые были
> полностью переписаны на Go.
>
> **Не используйте этот файл.** Актуальная архитектура описана в:
> - [docs/ARCHITECTURE.md](../ARCHITECTURE.md) — обзор архитектуры Go-переписи
> - [docs/API.md](../API.md) — актуальный REST API (русский)
> - [docs/FEATURES.md](../FEATURES.md) — актуальный список возможностей

## 🎯 Что нужно передать:

### 1. Основной контекст:
- **PROJECT_CONTEXT.md** ← ГЛАВНЫЙ ФАЙЛ - полный контекст проекта

### 2. Backend файлы новой архитектуры (из new-architecture/):
- **lib/TunnelInterface.js**
- **lib/Peer.js**
- **lib/InterfaceManager.js**
- **routes/tunnel-interfaces.js**
- **README.md** (из new-architecture)

### 3. Опционально (если нужен старый код):
- **app-FINAL-FIX.js** (последняя рабочая версия app.js)
- **TunnelManager.js** (старый менеджер WAN Tunnels)
- **WanTunnel-WITH-ADDRESS.js** (старый класс с Tunnel Address)

---

## 💬 Что написать новому Claude:

```
Привет! Продолжаю разработку AWG-Easy.

Контекст проекта в приложенном файле PROJECT_CONTEXT.md.

Текущая задача: интеграция новой архитектуры туннелей (Interface + Peers модель).

Созданы Backend классы (прилагаю):
- TunnelInterface.js
- Peer.js  
- InterfaceManager.js
- tunnel-interfaces.js (API routes)

Нужно:
1. Интегрировать Backend в проект
2. Создать Frontend UI
3. Протестировать

Git ветка: feature/wan-tunnels

Вопросы:
- Какой UI дизайн использовать для Interfaces/Peers?
- Нужна ли функция Import Config?
```

---

## 📋 Чеклист:

- [ ] Загрузить PROJECT_CONTEXT.md
- [ ] Загрузить все файлы из new-architecture/
- [ ] Скопировать промпт выше в новый чат
- [ ] Дождаться ответа нового Claude
- [ ] Продолжить разработку

---

## ⚡ Быстрый старт для нового Claude:

**Первый вопрос который нужно задать новому Claude:**

```
Прочитай PROJECT_CONTEXT.md и скажи что понял.
Какие следующие шаги ты рекомендуешь?
```

---

## 🎯 Цель после передачи:

1. ✅ Интегрировать Backend (скопировать файлы, добавить routes)
2. ✅ Создать Frontend UI (определить дизайн, создать компоненты)
3. ✅ Протестировать (API + UI)
4. ✅ Commit & Push в feature/wan-tunnels

---

**Удачи!** 🚀
