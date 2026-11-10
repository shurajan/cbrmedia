# cbrmedia — monorepo

Личная медиа-платформа: Go-бэкенд записывает живые HLS-стримы и раздаёт их iPad-плееру.
Язык документации, комментариев и сообщений коммитов — русский.

Текущая версия 1.3.1 (тег `v1.3.1`; исправления — `v1.3.x`, новые функции — `v1.4.0` и далее): функционал и дизайн зафиксированы. Дальше — только исправления ошибок и то, что
владелец попросит явно; поведение, меняющее логику, согласовывать.

| Папка | Что | Детали |
|-------|-----|--------|
| `services/` | Go 1.26, независимые службы `recorder`, `tbot`, `api`, `converter` + библиотека `core` (общая база SQLite) | `services/CLAUDE.md` |
| `apps/ios/` | CBRPlayer (на экране — «cbr») — SwiftUI-клиент для iPad, iOS 26.6, тёмная тема на палитре `Theme` | `apps/ios/CLAUDE.md` |
| `apps/android`, `apps/web` | планируются, пока нет — делать по макету из `design/` | — |
| `design/` | эталон дизайна: `tokens.json` (палитра и роли, генерируется из `Theme.swift` скриптом `gen_tokens.py`), артборды экранов `canvas/*.dc.html` | `design/README.md` |

## Проблемы и доработки

База для решений по доработкам — два файла в корне репо. Читать оба перед проверкой проекта и перед тем,
как предлагать или планировать исправления:

- `KNOWN_ISSUES.md` — найденное при проверках и принятое к устранению; отсюда берутся задачи на исправления.
  Исправленный пункт из файла удалять.
- `IGNORED_ISSUES.md` — то, что владелец решил не исправлять, и поведение, принятое в документации. В отчёты
  проверок не выносить; пункт возвращается в работу только по явной просьбе владельца или если изменился код,
  на который он ссылается. Новый пункт — только по решению владельца, с датой.

Как доработка меняет версию `X.Y.Z`:

- исправление ошибки — следующая минорная версия, `X.Y.Z+1` (после `v1.3.0` — `v1.3.1`);
- новая функция — следующая мажорная версия, `X.Y+1.0` (после `v1.3.x` — `v1.4.0`).

## Поток данных

```
tbot (Telegram) / api (iOS) ──пишут──▶ cbr.db: watchlist ──▶ recorder ──опрос status API, HLS──▶ ~/.cbr/recordings
                    ▲                                            │
                    └──── events, watch_state ◀──────────────────┘
converter (ffmpeg -c copy, каждые 5 мин; в dev.sh — 1 мин): recordings ──▶ ~/.cbr/media/<имя>/playlist.m3u8 + meta.json
все службы ──heartbeat──▶ cbr.db: services ──▶ api GET /health; api/tbot без живого recorder отвечают на добавление сразу
CBRPlayer ──HTTP :8000 (Bearer)──▶ api ──▶ ~/.cbr/media
CBRPlayer ──GET /watchlist/:user/live──▶ api ──status API──▶ hls_source ──▶ CBRPlayer играет прямо с CDN
```

## Контракт API (менять синхронно в Go и Swift)

- `POST /auth {"password"}` → `{"token"}` (один действующий токен; новый логин инвалидирует старый); ошибки, как и у
  остальных маршрутов, — JSON `{"error"}` (Go `jsonError` ↔ Swift `AuthErrorResponse`, текст показывается на экране входа)
- `GET /videos` → `[{id, name, hlsURL, createdAt?, duration, resolution?, sizeMB?, segmentCount, avgSegmentDuration}]`
  — Go: `services/api/internal/httpapi/videos.go` `MediaFile`; Swift: `Models/VideoItemModel.swift`. Метаданные — из
  `media/<имя>/meta.json` (Go `core/media.Meta`, пишет converter при публикации и дописывает старым видео при очередном
  проходе); api плейлист не разбирает и ffprobe не запускает, видео без meta.json отдаётся с нулями
- `GET /videos/:name/*` — файлы HLS; `DELETE /videos/:name`
- `GET/POST /watchlist`, `DELETE /watchlist/:user` — экран «Watchlist» в iOS (открывается плиткой с главной)
  (Go `WatchEntry` в `watchlist.go` ↔ Swift `Models/WatchEntryModel.swift`). `POST` и `/add` в tbot принимают имя
  или ссылку на страницу, сохраняется только имя в нижнем регистре (`DELETE` и `/del` тоже приводят имя к нижнему) (Go `store.UserFromInput` ↔ Swift `WatchlistViewModel.userName(from:)`).
  Ответ на добавление — после первой проверки recorder'ом (5–8 с): 404, если пользователя нет; 201 `{user, check, …}`
  (Go `checkNames` ↔ Swift `Models/WatchAddResultModel.swift`), `check: "recorder_offline"` — recorder не запущен
  (нет heartbeat), ответ сразу, iOS показывает уведомление под полем.
  `roomStatus` `blocked` / `not found` (Go `store.Room*`) ↔ Swift `WatchEntryModel.Status.blocked/.notFound`
- `GET /watchlist/:user/live` → `{user, roomStatus, hlsURL}` — свежая ссылка на живой стрим (api спрашивает status API;
  нужен `status_base_url` в `api.json`); 409, если не `public` (Go `LiveStream` в `live.go` ↔ Swift `Models/LiveStreamModel.swift`,
  409 → `NetworkServiceError.conflict`). Плеер открывает `hlsURL` напрямую на CDN, без Bearer. Кнопка play в Watchlist —
  у `roomStatus == "public"` (Go `roomPublic` ↔ Swift `WatchEntryModel.isLive`)
- `GET /events?after=` — пока не используется клиентами; полная таблица — `services/api/README.md`
- `GET /health` → `{version, services: [{name, version, alive, seenAt, interval}]}` — heartbeat всех служб из общей базы
  (Go `Health` в `health.go`); клиентами пока не используется — для диагностики с любого устройства в сети
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
