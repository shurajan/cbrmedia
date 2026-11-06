# cbrmedia — monorepo

Личная медиа-платформа: Go-бэкенд записывает живые HLS-стримы и раздаёт их iPad-плееру.
Язык документации, комментариев и сообщений коммитов — русский.

Версия 1.0 (тег `v1.0`, исправления — `v1.0.x`): функционал и дизайн зафиксированы. Дальше — только исправления ошибок и то, что
владелец попросит явно; поведение, меняющее логику, согласовывать.

| Папка | Что | Детали |
|-------|-----|--------|
| `services/` | Go 1.26, независимые службы `recorder`, `tbot`, `api`, `converter` + библиотека `core` (общая база SQLite) | `services/CLAUDE.md` |
| `apps/ios/` | CBRPlayer (на экране — «cbr») — SwiftUI-клиент для iPad, iOS 26.6, тёмная тема на палитре `Theme` | `apps/ios/CLAUDE.md` |
| `apps/android`, `apps/web` | планируются, пока нет | — |

## Поток данных

```
tbot (Telegram) / api (iOS) ──пишут──▶ cbr.db: watchlist ──▶ recorder ──опрос status API, HLS──▶ ~/.cbr/recordings
                    ▲                                            │
                    └──── events, watch_state ◀──────────────────┘
converter (ffmpeg -c copy, каждые 5 мин; в dev.sh — 1 мин): recordings ──▶ ~/.cbr/media/<имя>/playlist.m3u8
CBRPlayer ──HTTP :8000 (Bearer)──▶ api ──▶ ~/.cbr/media
```

## Контракт API (менять синхронно в Go и Swift)

- `POST /auth {"password"}` → `{"token"}` (один действующий токен; новый логин инвалидирует старый)
- `GET /videos` → `[{id, name, hlsURL, createdAt?, duration, resolution?, sizeMB?, segmentCount, avgSegmentDuration}]`
  — Go: `services/api/internal/httpapi/videos.go` `MediaFile`; Swift: `Models/VideoItemModel.swift`
- `GET /videos/:name/*` — файлы HLS; `DELETE /videos/:name`
- `GET/POST /watchlist`, `DELETE /watchlist/:user` — вкладка «Watchlist» в iOS
  (Go `WatchEntry` в `watchlist.go` ↔ Swift `Models/WatchEntryModel.swift`). `POST` и `/add` в tbot принимают имя
  или ссылку на страницу, сохраняется только имя в нижнем регистре (`DELETE` и `/del` тоже приводят имя к нижнему) (Go `store.UserFromInput` ↔ Swift `WatchlistViewModel.userName(from:)`).
  Ответ на добавление — после первой проверки recorder'ом (5–8 с): 404, если пользователя нет.
  `roomStatus` `blocked` / `not found` (Go `store.Room*`) ↔ Swift `WatchEntryModel.Status.blocked/.notFound`
- `GET /events?after=` — пока не используется клиентами; полная таблица — `services/api/README.md`
- Всё, кроме `/auth`, требует `Authorization: Bearer <token>`. Транспорт — plain HTTP (`http://host:port`).

## Запуск

- Весь back-end локально (macOS): `services/dev.sh [служба…]` — данные в `~/cbr-dev` (`CBR_HOME`), Ctrl+C останавливает всё.
- Диагностика внешних сервисов: `services/dev_probe.sh <user|ссылка>` (пользователь в эфире) — сырые ответы status API
  и CDN с замаскированными ссылками в `hlsprobe-<время>/` в корне репо; по ним подстраивается recorder (`services/recorder/README.md`).
- Тесты и проверки: `make -C services test` / `make -C services check` (vet, golangci-lint, тесты, govulncheck).
- Сервер (Ubuntu): `services/deploy/install.sh [служба…]` — systemd-units `cbr-<служба>.service`.
- iOS: `open apps/ios/CBRPlayer.xcodeproj`, симулятор iPad с iOS ≥ 26.6; хост в приложении `localhost:8000`.

## Общее

- Единый `.gitignore` в корне (секции: общие / iOS / Go). Игнорирует конфиги служб с токенами, служебную папку
  внутри репо (`.cbr/`, `cbr-dev/`), файлы SQLite, бинарники от `go build` (перечисляет по именам служб и `hlsprobe`)
  результаты `hlsprobe-*/` (в них сегменты и ключи стримов) и `/recordings/` в корне — записи, положенные для разбора.
- Секреты (`<служба>.json`, `auth.json`) живут вне репо в `$CBR_HOME/config/`.
- Службы независимы: включаются/выключаются отдельно (`cbr-<служба>.service`), общаются только через
  общую базу `~/.cbr/data/cbr.db` и файлы.
- История: объединено из репозиториев FSPlayer, mediafs, Watcher (ветки с префиксами `fsplayer/…`, `mediafs/…`, `watcher/…`).
