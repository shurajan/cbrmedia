# services (Go)

Обзор для людей — `README.md`, поведение служб — `<служба>/README.md`. Здесь — карта кода для доработок.

## Модули

Каждая папка — отдельный Go-модуль `cbr/<имя>` (Go 1.26), `replace cbr/core => ../core`.
Импорты: `cbr/core/...`, `cbr/<служба>/internal/...` (goimports local-prefix `cbr/`).

Общее для всех модулей лежит в `services/`: `Makefile` (цель по всем `MODULES`), `common.mk` (цели модуля,
подключается из `<модуль>/Makefile` с `SERVICE`/`RUN_ARGS`), `.golangci.yml` (golangci-lint находит его из
подпапок), `dev.sh` (локальный запуск), `deploy/` (`install.sh` + `logrotate.conf`).

| Модуль | Пакеты |
|--------|--------|
| `core` | `home` (`Layout`, `Prepare()` = resolve+mkdir+chdir, `ConfigFile(svc)`, `AuthFile()`, `DBFile()`), `store` (SQLite; `user.go` — `UserFromInput`), `config` (`Load(path, Validator)`, `ValidateURL`), `svc` (`Logger`, `SignalContext`), `ctxutil.Sleep` |
| `recorder` | `internal/app` (`Run`: ClearStates → RecoverPartial → цикл `Sync` раз в 3 с, prune событий раз в час), `internal/recorder` (`Manager.Sync(ctx, desired)`, `watch`, `record`, `classifyFailure` в `delay.go`: 404/410 → неподтверждённого сразу `reject` (`user_rejected`), подтверждённого (`verified`) — `RoomNotFound` и опрос раз в сутки, после `notFoundLimit` = 3 подряд (счётчик в памяти) → `remove`; 401/403/451 → `RoomBlocked` и опрос раз в сутки, 429/5xx/сеть → повтор, прочие 4xx → `disable`; `Sink` = store; `status.go` StatusClient + `NewHTTPClient` с выключенным TLS-verify; `delay.go` интервалы), `internal/hls` (свой HLS-рекордер: `playlist.go`, `crypto.go` AES-128, `writer.go` part-файлы `.download` с правами `0o600` и ротацией раз в час, `RecoverPartial`, `recorder.go` `Record`/`DetectFormat`), `internal/config` |
| `tbot` | `internal/telegram` (`Bot`: `/add /del /list` — имя или ссылка через `store.UserFromInput`, ответ с итоговым именем; inline-меню удаления, `/add` ждёт `WaitFirstCheck` и отвечает `addReply`, поэтому `WithWorkers(4)`; `ForwardEvents` раз в 3 с с курсором `tbot` — в чат только `forwardKinds` (сейчас `user_disabled`, `user_removed`; о записях бот не пишет намеренно), `formatEntry`, `chunk`), `internal/app`, `internal/config` |
| `api` | `internal/httpapi` (fiber v3: `server.go` маршруты, `auth.go` bearer, `videos.go`, `watchlist.go` `/watchlist` (POST принимает ссылку, ждёт `WaitFirstCheck`, отвечает `{message, user, check}` или 404 для отклонённого) + `/events`, `reqlog.go` — лог каждого запроса через slog, подключается только при `-debug`), `internal/auth` (bcrypt + один uuid-токен в auth.json), `internal/library` (метаданные видео из m3u8, fallback ffprobe), `internal/app`, `internal/config`; `cmd/api hash-password` |
| `converter` | `internal/converter` (`Run` каждые `-interval`, по умолчанию 5 мин, в `dev.sh` 1 мин; `RunOnce`: `cleanupTemp` → `pending` → `remux` в `media/.convert-<имя>` → `validatePlaylist` → rename / `recordings/failed/`), `cmd/converter` (flock `data/converter.lock`). Базу не использует |

## Общая база (`core/store`, `~/.cbr/data/cbr.db`)

- Таблицы: `watchlist(user, added_at, added_by, enabled, disabled_reason, verified)`, `watch_state(user, room_status, recording, updated_at)`,
  `events(id, at, kind, user, message)`, `cursors(consumer, event_id)`. Время — unix ms.
- Миграции — срез `migrations` по `PRAGMA user_version`: только дописывать в конец.
- Кто пишет: watchlist — tbot/api (`AddUser/RemoveUser`) и recorder (`DisableUser`, `RemoveUser` при 404/410, `MarkVerified` при первом успешном ответе status API); watch_state и events — только recorder.
- `ValidUser`: `^[A-Za-z0-9_.-]{1,64}$` (имя попадает в URL и в имена файлов).
- `UserFromInput` (`store/user.go`): имя или ссылка → имя (последний непустой сегмент пути, схема необязательна,
  query/fragment отбрасываются). Вызывают tbot (`/add`, `/del`) и api (`POST /watchlist`); в базу пишется только имя.
  Те же правила в iOS — `WatchlistViewModel.userName(from:)`, менять синхронно.
- События: `recording_started|finished|failed`, `user_disabled`, `user_removed`, `user_rejected` — константы `store.Kind*`.
- `WaitFirstCheck` (`store/check.go`): после `AddUser` api и tbot ждут первую проверку recorder'ом (до `FirstCheckTimeout` = 30 с,
  опрос базы раз в 0,5 с) — признак: запись в watch_state, выключение или удаление. Отклонённого (`CheckRejected`)
  recorder удаляет с событием `user_rejected`, tbot его не пересылает — добавивший видит итог в ответе.
  `store.RoomBlocked` (`blocked`, 401/403/451) и `store.RoomNotFound` (`not found`, 404/410) — room_status,
  который ставит recorder при таких ошибках; tbot показывает их в `/list` как 🚫 и ❓.

## Команды

```bash
make test | make check | make build   # из services/ — по всем модулям; из папки модуля — по нему
./dev.sh [служба…]                    # собрать и запустить вместе локально (CBR_HOME=~/cbr-dev, DEBUG=1)
CBR_HOME=~/cbr-dev make -C tbot run   # одну службу через go run
```

`dev.sh` (bash 3.2 из macOS: без ассоциативных массивов и `wait -n`) и `deploy/install.sh` проверяют одно и то же
перед запуском: конфиг `<служба>.json` (нет — кладут шаблон и пропускают службу), `auth.json` у api, ffmpeg у converter.
Меняешь набор служб или их аргументы — правь оба скрипта.

## Соглашения

- Службы не импортируют друг друга и не общаются по сети между собой — только `core/store` и файлы в `~/.cbr`.
- Новый интерфейс управления = новый модуль: пишет `watchlist`, читает `events` со своим `Consumer` в `cursors`.
- У службы свой `~/.cbr/config/<служба>.json` (`config.example.json` в папке модуля), бинарник `cmd/<служба>`,
  unit `cbr-<служба>.service`, лог `logs/<служба>.log`. Новую службу добавить в `deploy/install.sh` (`ALL_SERVICES`,
  `preflight`), `dev.sh` (`ALL_SERVICES`, `ready`, `args`), `Makefile` (`MODULES`) и бинарники в корневой `.gitignore`.
- `log/slog`, остановка через `context`, без глобального состояния; комментарии и документация на русском;
  тесты рядом с кодом, база в тестах — настоящая SQLite во временной папке.
- Изменил JSON `/videos` или `/watchlist` → обнови `apps/ios/CBRPlayer/Models/` и таблицу в `api/README.md`.
- Права: служебная папка и подпапки `0o750` (`home.Ensure`, `chmod 750` в `dev.sh`/`install.sh`), секреты и записи `0o600`.
  Все службы работают от одного пользователя (`User=` в unit), поэтому шире права не нужны.
- Зависимости: при `go mod tidy` следи, чтобы версии не откатывались (fiber v3.3.0, go-telegram/bot v1.20.0,
  modernc.org/sqlite v1.61.1). `go mod verify` ругается на локальный `cbr/core` (replace) — это нормально.
- Уязвимость в косвенной зависимости (govulncheck в `make check`) — точечно `go get <модуль>@<fixed>` в нужном модуле,
  без `go get -u ./...`: иначе сдвинутся закреплённые версии. Так обновлены brotli 1.2.7 и klauspost/compress 1.20.2 в api.
