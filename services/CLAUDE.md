# services (Go)

Обзор для людей — `README.md`, поведение служб — `<служба>/README.md`. Здесь — карта кода для доработок.

## Модули

Каждая папка — отдельный Go-модуль `cbr/<имя>` (Go 1.26), `replace cbr/core => ../core`.
Импорты: `cbr/core/...`, `cbr/<служба>/internal/...` (goimports local-prefix `cbr/`).

Общее для всех модулей лежит в `services/`: `Makefile` (цель по всем `MODULES`), `common.mk` (цели модуля,
подключается из `<модуль>/Makefile` с `SERVICE`/`RUN_ARGS`), `.golangci.yml` (golangci-lint находит его из
подпапок), `dev.sh` (локальный запуск), `dev_probe.sh` (hlsprobe с настройками dev), `deploy/` (`install.sh` + `logrotate.conf`).

| Модуль | Пакеты |
|--------|--------|
| `core` | `home` (`Layout`, `Prepare()` = resolve+mkdir+chdir, `ConfigFile(svc)`, `AuthFile()`, `DBFile()`), `store` (SQLite; `user.go` — `UserFromInput`; `check.go` — `WaitFirstCheck`), `config` (`Load(path, Validator)`, `ValidateURL`), `svc` (`Logger`, `SignalContext`), `ctxutil.Sleep` |
| `recorder` | `internal/app` (`Run`: ClearStates → RecoverPartial → цикл `Sync` раз в 3 с, prune событий раз в час), `internal/recorder` (`Manager.Sync(ctx, desired)`, `watch`, `record`, `classifyFailure` в `delay.go`: 404/410 → неподтверждённого сразу `reject` (`user_rejected`), подтверждённого (`verified`) — `RoomNotFound` и опрос раз в сутки, после `notFoundLimit` = 3 подряд (счётчик в памяти) → `remove`; 401/403/451 → `RoomBlocked` и опрос раз в сутки, 5xx/сеть → `techFailure`, 429 → `Retry-After` (`StatusError`) или `rateLimited`, прочие 4xx → `disable`; `record` возвращает `recordSkipped/Done/Short/Failed`: после долгой записи (`longRecording`) опрос сразу, после неудачной — `recordRetryDelay` (15 с ×2 до 15 мин); `Sink` = store; `status.go` StatusClient (таймаут 30 с, тело ошибки обрезается) + `NewHTTPClient` с выключенным TLS-verify; `delay.go` интервалы), `internal/hls` (свой HLS-рекордер: `playlist.go`, `crypto.go` AES-128, `writer.go` part-файлы `.download` с правами `0o600` и ротацией раз в час, `RecoverPartial`, `recorder.go` `Record`/`DetectFormat`: таймауты запроса `playlistTimeout`/`segmentTimeout`, сегменты строго по порядку — неудачный повторяется, следующие ждут, после `maxSegmentAttempts` = 3 пропускается; ни одного сегмента → `ErrNoSegments`), `internal/config`; `cmd/hlsprobe` — диагностика status API и CDN (ответы с замаскированными ссылками — `mask.go` — в `hlsprobe-<время>/`, см. README) |
| `tbot` | `internal/telegram` (`Bot`: `/add /del /list` — имя или ссылка через `store.UserFromInput`, ответ с итоговым именем; inline-меню удаления, `/add` ждёт `WaitFirstCheck` и отвечает `addReply`, поэтому `WithWorkers(4)`; `ForwardEvents` раз в 3 с с курсором `tbot` — в чат только `forwardKinds` (сейчас `user_disabled`, `user_removed`; о записях бот не пишет намеренно), `formatEntry`, `chunk`), `internal/app`, `internal/config` |
| `api` | `internal/httpapi` (fiber v3: `server.go` маршруты, `auth.go` bearer, `videos.go`, `watchlist.go` `/watchlist` (POST принимает ссылку, ждёт `WaitFirstCheck`, отвечает `{message, user, check}` или 404 для отклонённого) + `/events`, `reqlog.go` — лог каждого запроса через slog, подключается только при `-debug`), `internal/auth` (bcrypt + один uuid-токен в auth.json), `internal/library` (метаданные видео из m3u8, fallback ffprobe), `internal/app`, `internal/config`; `cmd/api hash-password` |
| `converter` | `internal/converter` (`Run` каждые `-interval`, по умолчанию 5 мин, в `dev.sh` 1 мин; `RunOnce`: `cleanupTemp` → `pending` → `remux` в `media/.convert-<имя>` → `validatePlaylist` → rename / `recordings/failed/`), `cmd/converter` (flock `data/converter.lock`). Базу не использует |

## Общая база (`core/store`, `~/.cbr/data/cbr.db`)

- Таблицы: `watchlist(user, added_at, added_by, enabled, disabled_reason, verified)`, `watch_state(user, room_status, recording, updated_at)`,
  `events(id, at, kind, user, message)`, `cursors(consumer, event_id)`. Время — unix ms.
- Миграции — срез `migrations` по `PRAGMA user_version`: только дописывать в конец.
- Кто пишет: watchlist — tbot/api (`AddUser/RemoveUser`) и recorder (`DisableUser`, `RemoveUser` при 404/410, `MarkVerified` при первом успешном ответе status API); watch_state и events — только recorder.
- `ValidUser`: `^[A-Za-z0-9_.-]{1,64}$` (имя попадает в URL и в имена файлов).
- `UserFromInput` (`store/user.go`): имя или ссылка → имя в нижнем регистре (последний непустой сегмент пути,
  схема необязательна, query/fragment отбрасываются). Миграция 3 привела к нижнему регистру уже сохранённые имена. Вызывают tbot (`/add`, `/del`) и api (`POST /watchlist`); в базу пишется только имя.
  Те же правила в iOS — `WatchlistViewModel.userName(from:)`, менять синхронно.
- События: `recording_started|finished|failed`, `user_disabled`, `user_removed`, `user_rejected` — константы `store.Kind*`.
- `verified` (миграция 2): status API хоть раз ответил о пользователе успешно (`Verified`/`MarkVerified`, ставит recorder).
  Только подтверждённых защищает счётчик 3× 404; при миграции все существовавшие строки помечены подтверждёнными.
- `store.RoomBlocked` (`blocked`, 401/403/451) и `store.RoomNotFound` (`not found`, 404/410) — room_status, который ставит
  recorder при таких ошибках; tbot показывает их в `/list` как 🚫 и ❓, iOS — `WatchEntryModel.Status.blocked/.notFound`.
- `WaitFirstCheck` (`store/check.go`): после `AddUser` api и tbot ждут первую проверку recorder'ом (до `FirstCheckTimeout` = 30 с,
  опрос базы раз в 0,5 с). Признак проверки — строка в watch_state, выключение или удаление пользователя, поэтому recorder
  обязан писать watch_state (или выключать/удалять) уже при первом ответе status API. Обычно 5–8 с: `DefaultSyncInterval`
  (3 с) + `initialDelay` (5 с) в recorder — меняешь их, меняется и задержка ответа. Отклонённого (`CheckRejected`)
  recorder удаляет с событием `user_rejected`, tbot его не пересылает — добавивший видит итог в ответе.

## Команды

```bash
make test | make check | make build   # из services/ — по всем модулям; из папки модуля — по нему
./dev.sh [служба…]                    # собрать и запустить вместе локально (CBR_HOME=~/cbr-dev, DEBUG=1)
./dev_probe.sh <user|ссылка>          # hlsprobe с настройками dev и готовыми параметрами → hlsprobe-<время>/ в корне репо
CBR_HOME=~/cbr-dev make -C tbot run   # одну службу через go run
```

`dev.sh` (bash 3.2 из macOS: без ассоциативных массивов и `wait -n`) и `deploy/install.sh` проверяют одно и то же
перед запуском: конфиг `<служба>.json` (нет — кладут шаблон и пропускают службу), `auth.json` у api, ffmpeg у converter.
Меняешь набор служб или их аргументы — правь оба скрипта.

`dev_probe.sh` принимает только пользователя, параметры hlsprobe заданы в нём (`-nonexistent neverexistinguserbla`,
`-wait 5m`, `-duration 5m`, `-segments 4`); собирает `$CBR_HOME/bin/hlsprobe` и запускает из корня репо. Нужен новый
замер — добавь флаг в `cmd/hlsprobe` и значение в скрипт, а не аргументы пользователю.

Замер 2026-11-06 (`dev_probe.sh`, один пользователь в эфире):
- несуществующий пользователь → 404; имя в верхнем регистре → 404: status API различает регистр, имена в нижнем — верно;
- `hls_source` при каждом запросе новый (другой edge, новый `session` в query); master: 4 варианта до 1080p
  (5,1 Мбит/с), **звук — отдельные дорожки `#EXT-X-MEDIA`**, варианты — только видео. Recorder пишет лучший вариант,
  поэтому записи без звука (подтверждено `ffprobe` на dev-записях) — не исправлено;
- LL-HLS, fMP4 (`#EXT-X-MAP`, `.m4s`), target 2 с, в плейлисте всего 4 сегмента (окно 8 с), сегмент ~1 МБ качается
  0,6–1 с; без шифрования;
- токен `session` за 5 минут перезагрузок не менялся и не истёк: сравнение сегментов по полному URL работает,
  переходить на `MEDIA-SEQUENCE` не нужно.

Не проверено: коды для заблокированного пользователя (401/403), `Retry-After` на 429, срок жизни `session` дольше 5 минут.

## Соглашения

- Службы не импортируют друг друга и не общаются по сети между собой — только `core/store` и файлы в `~/.cbr`.
- Новый интерфейс управления = новый модуль: пишет `watchlist`, читает `events` со своим `Consumer` в `cursors`.
- У службы свой `~/.cbr/config/<служба>.json` (`config.example.json` в папке модуля), бинарник `cmd/<служба>`,
  unit `cbr-<служба>.service`, лог `logs/<служба>.log`. Новую службу добавить в `deploy/install.sh` (`ALL_SERVICES`,
  `preflight`), `dev.sh` (`ALL_SERVICES`, `ready`, `args`), `Makefile` (`MODULES`) и бинарники в корневой `.gitignore`.
- `log/slog`, остановка через `context`, без глобального состояния; комментарии и документация на русском;
  тесты рядом с кодом, база в тестах — настоящая SQLite во временной папке.
- Схема базы и протокол «api/tbot ждут recorder» общие для всех служб — на сервере обновлять службы вместе
  (`install.sh` без аргументов). Новый api/tbot со старым recorder будут отвечать на добавление только через 30 с.
- Изменил JSON `/videos` или `/watchlist` → обнови `apps/ios/CBRPlayer/Models/` и таблицу в `api/README.md`.
- Права: служебная папка и подпапки `0o750` (`home.Ensure`, `chmod 750` в `dev.sh`/`install.sh`), секреты и записи `0o600`.
  Все службы работают от одного пользователя (`User=` в unit), поэтому шире права не нужны.
- Зависимости: при `go mod tidy` следи, чтобы версии не откатывались (fiber v3.3.0, go-telegram/bot v1.20.0,
  modernc.org/sqlite v1.61.1). `go mod verify` ругается на локальный `cbr/core` (replace) — это нормально.
- Уязвимость в косвенной зависимости (govulncheck в `make check`) — точечно `go get <модуль>@<fixed>` в нужном модуле,
  без `go get -u ./...`: иначе сдвинутся закреплённые версии. Так обновлены brotli 1.2.7 и klauspost/compress 1.20.2 в api.
