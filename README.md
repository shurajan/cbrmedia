# cbrmedia

Monorepo медиа-платформы: серверы и клиенты.

```
cbrmedia/
├── apps/               # клиентские приложения
│   ├── ios/            # CBRPlayer — iPad-клиент (Swift / SwiftUI, Xcode)
│   ├── android/        # (планируется)
│   └── web/            # (планируется)
└── services/           # бэкенд (каждый сервис — отдельный Go-модуль)
    └── cbrservice/     # запись стримов + Telegram-бот + HTTP-раздача видео (Go)
```

## Быстрый старт

| Проект | Команда |
|--------|---------|
| iOS    | `open apps/ios/CBRPlayer.xcodeproj` → схема **CBRPlayer**, симулятор iPad |
| cbrservice | `services/cbrservice/start.sh` (деплой на Ubuntu: `services/cbrservice/deploy/cbrservice.sh`) |

## История

Проекты объединены из отдельных репозиториев с сохранением истории:

| Папка | Бывший репозиторий |
|-------|--------------------|
| `apps/ios/` | github.com/shurajan/FSPlayer |
| `services/cbrservice/` | github.com/shurajan/mediafs + github.com/shurajan/Watcher (объединены) |

Старые ветки перенесены с префиксом проекта: `fsplayer/…`, `mediafs/…`, `watcher/…`.
