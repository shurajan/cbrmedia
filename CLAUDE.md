# cbrmedia — monorepo

Личная медиа-платформа: Go-бэкенд записывает живые HLS-стримы и раздаёт их iPad-плееру.
Язык документации, комментариев и сообщений коммитов — русский.

| Папка | Что | Детали |
|-------|-----|--------|
| `services/` | Go 1.26, независимые службы `recorder`, `tbot`, `api`, `converter` + библиотека `core` (общая база SQLite) | `services/CLAUDE.md` |
| `apps/ios/` | CBRPlayer — SwiftUI-клиент для iPad (Xcode, без внешних зависимостей) | `apps/ios/CLAUDE.md` |
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
- `GET/POST /watchlist`, `DELETE /watchlist/:user`, `GET /events?after=` — для будущего UI управления
  (в iOS пока не используются); полная таблица — `services/api/README.md`
- Всё, кроме `/auth`, требует `Authorization: Bearer <token>`. Транспорт — plain HTTP (`http://host:port`).

## Запуск

- Весь back-end локально (macOS): `services/dev.sh [служба…]` — данные в `~/cbr-dev` (`CBR_HOME`), Ctrl+C останавливает всё.
- Тесты и проверки: `make -C services test` / `make -C services check` (vet, golangci-lint, тесты, govulncheck).
- Сервер (Ubuntu): `services/deploy/install.sh [служба…]` — systemd-units `cbr-<служба>.service`.
- iOS: `open apps/ios/CBRPlayer.xcodeproj`; в симуляторе хост `localhost:8000`.

## Общее

- Единый `.gitignore` в корне (секции: общие / iOS / Go). Игнорирует конфиги служб с токенами, служебную папку
  внутри репо (`.cbr/`, `cbr-dev/`), файлы SQLite и бинарники от `go build` — их перечисляет по именам служб.
- Секреты (`<служба>.json`, `auth.json`) живут вне репо в `$CBR_HOME/config/`.
- Службы независимы: включаются/выключаются отдельно (`cbr-<служба>.service`), общаются только через
  общую базу `~/.cbr/data/cbr.db` и файлы.
- История: объединено из репозиториев FSPlayer, mediafs, Watcher (ветки с префиксами `fsplayer/…`, `mediafs/…`, `watcher/…`).
