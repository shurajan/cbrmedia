# cbrmedia

Monorepo медиа-платформы: серверы и клиенты.

```
cbrmedia/
├── apps/               # клиентские приложения
│   ├── ios/            # CBRPlayer — iPad-клиент (Swift / SwiftUI, Xcode)
│   ├── android/        # (планируется)
│   └── web/            # (планируется)
└── services/           # бэкенд (каждый сервис — отдельный Go-модуль)
    ├── core/           # общая библиотека: служебная папка ~/.cbr, база SQLite
    ├── recorder/       # запись стримов
    ├── tbot/           # Telegram-бот управления
    ├── api/            # HTTP API для плеера
    ├── converter/      # recordings → HLS в медиатеку (ffmpeg)
    └── deploy/         # установка служб на Ubuntu
```

## Быстрый старт

| Проект | Команда |
|--------|---------|
| iOS    | `open apps/ios/CBRPlayer.xcodeproj` → схема **CBRPlayer**, симулятор iPad |
| services | локально `services/dev.sh`, тесты `make -C services test`, деплой на Ubuntu `services/deploy/install.sh` — см. [services/README.md](services/README.md) |

## История

Проекты объединены из отдельных репозиториев с сохранением истории:

| Папка | Бывший репозиторий |
|-------|--------------------|
| `apps/ios/` | github.com/shurajan/FSPlayer |
| `services/` | github.com/shurajan/mediafs (→ api) + github.com/shurajan/Watcher (→ recorder, tbot) |

Старые ветки перенесены с префиксом проекта: `fsplayer/…`, `mediafs/…`, `watcher/…`.
