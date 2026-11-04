# services (Go)

Обзор для людей — `README.md`, поведение служб — `<служба>/README.md`. Здесь — карта кода для доработок.

## Модули

Каждая папка — отдельный Go-модуль `cbr/<имя>` (Go 1.26), `replace cbr/core => ../core`.
Импорты: `cbr/core/...`, `cbr/<служба>/internal/...` (goimports local-prefix `cbr/`).

| Модуль | Пакеты |
|--------|--------|
| `core` | `home` (`Layout`, `Prepare()` = resolve+mkdir+chdir, `ConfigFile(svc)`, `AuthFile()`, `DBFile()`), `store` (SQLite), `config` (`Load(path, Validator)`, `ValidateURL`), `svc` (`Logger`, `SignalContext`), `ctxutil.Sleep` |
| `recorder` | `internal/app` (`Run`: ClearStates → RecoverPartial → цикл `Sync` раз в 3 с, prune событий раз в час), `internal/recorder` (`Manager.Sync(ctx, desired)`, `watch`, `record`, `disable` при 4xx; `Sink` = store; `status.go` StatusClient + `NewHTTPClient` с выключенным TLS-verify; `delay.go` интервалы), `internal/hls` (свой HLS-рекордер: `playlist.go`, `crypto.go` AES-128, `writer.go` part-файлы `.download`, `RecoverPartial`, `recorder.go` `Record`/`DetectFormat`), `internal/config` |
| `tbot` | `internal/telegram` (`Bot`: `/add /del /list`, inline-меню удаления, `ForwardEvents` с курсором `tbot` — в чат только `forwardKinds` (сейчас `user_disabled`), `formatEntry`, `chunk`), `internal/app`, `internal/config` |
| `api` | `internal/httpapi` (fiber v3: `server.go` маршруты, `auth.go` bearer, `videos.go`, `watchlist.go` `/watchlist` + `/events`), `internal/auth` (bcrypt + один uuid-токен в auth.json), `internal/library` (метаданные видео из m3u8, fallback ffprobe), `internal/app`, `internal/config`; `cmd/api hash-password` |
| `converter` | `internal/converter` (`RunOnce`: `cleanupTemp` → `pending` → `remux` в `media/.convert-<имя>` → `validatePlaylist` → rename / `recordings/failed/`), `cmd/converter` (flock `data/converter.lock`). Базу не использует |

## Общая база (`core/store`, `~/.cbr/data/cbr.db`)

- Таблицы: `watchlist(user, added_at, added_by, enabled, disabled_reason)`, `watch_state(user, room_status, recording, updated_at)`,
  `events(id, at, kind, user, message)`, `cursors(consumer, event_id)`. Время — unix ms.
- Миграции — срез `migrations` по `PRAGMA user_version`: только дописывать в конец.
- Кто пишет: watchlist — tbot/api (`AddUser/RemoveUser`) и recorder (`DisableUser`); watch_state и events — только recorder.
- `ValidUser`: `^[A-Za-z0-9_.-]{1,64}$` (имя попадает в URL и в имена файлов).
- События: `recording_started|finished|failed`, `user_disabled` — константы `store.Kind*`.

## Команды

```bash
make test | make check | make build   # из services/ — по всем модулям; из папки модуля — по нему
CBR_HOME=/tmp/cbr-dev make -C tbot run
```

## Соглашения

- Службы не импортируют друг друга и не общаются по сети между собой — только `core/store` и файлы в `~/.cbr`.
- Новый интерфейс управления = новый модуль: пишет `watchlist`, читает `events` со своим `Consumer` в `cursors`.
- У службы свой `~/.cbr/config/<служба>.json` (`config.example.json` в папке модуля), бинарник `cmd/<служба>`,
  unit `cbr-<служба>.service`, лог `logs/<служба>.log`. Новую службу добавить в `deploy/install.sh` (`ALL_SERVICES`,
  `preflight`) и в `Makefile` (`MODULES`).
- `log/slog`, остановка через `context`, без глобального состояния; комментарии и документация на русском;
  тесты рядом с кодом, база в тестах — настоящая SQLite во временной папке.
- Изменил JSON `/videos` или `/watchlist` → обнови `apps/ios/CBRPlayer/Models/` и таблицу в `api/README.md`.
- Зависимости: при `go mod tidy` следи, чтобы версии не откатывались (fiber v3.3.0, go-telegram/bot v1.20.0).
