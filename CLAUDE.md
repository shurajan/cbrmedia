# cbrmedia — monorepo

Личная медиа-платформа: Go-бэкенд записывает живые HLS-стримы и раздаёт их iPad-плееру.
Язык документации, комментариев и сообщений коммитов — русский.

| Папка | Что | Детали |
|-------|-----|--------|
| `services/cbrservice/` | Go 1.26 модуль `cbrservice`: recorder + Telegram-бот + HTTP API, и отдельная служба `cbrconvert` | `services/cbrservice/CLAUDE.md` |
| `apps/ios/` | CBRPlayer — SwiftUI-клиент для iPad (Xcode, без внешних зависимостей) | `apps/ios/CLAUDE.md` |
| `apps/android`, `apps/web` | планируются, пока нет | — |

## Поток данных

```
status API ──опрос──▶ cbrservice/recorder ──HLS──▶ ~/.cbr/recordings/*.ts|*.mp4
                         ▲ Telegram /add /del /list
cbrconvert (ffmpeg -c copy, каждые 5 мин) ──▶ ~/.cbr/media/<имя>/playlist.m3u8
CBRPlayer ──HTTP :8000 (Bearer)──▶ cbrservice/httpapi ──▶ ~/.cbr/media
```

## Контракт API (менять синхронно в Go и Swift)

- `POST /auth {"password"}` → `{"token"}` (один действующий токен; новый логин инвалидирует старый)
- `GET /videos` → `[{id, name, hlsURL, createdAt?, duration, resolution?, sizeMB?, segmentCount, avgSegmentDuration}]`
  — Go: `internal/httpapi/videos.go` `MediaFile`; Swift: `Models/VideoItemModel.swift`
- `GET /videos/:name/*` — файлы HLS; `DELETE /videos/:name`
- Всё, кроме `/auth`, требует `Authorization: Bearer <token>`. Транспорт — plain HTTP (`http://host:port`).

## Общее

- Единый `.gitignore` в корне (секции: общие / iOS / Go / cbrservice).
- Секреты (`config.json`, `auth.json`) живут вне репо в `~/.cbr/config/`.
- История: объединено из репозиториев FSPlayer, mediafs, Watcher (ветки с префиксами `fsplayer/…`, `mediafs/…`, `watcher/…`).
