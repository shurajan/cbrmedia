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
| `core` | `home` (`Layout`, `Prepare()` = resolve+mkdir+chdir, `ConfigFile(svc)`, `AuthFile()`, `DBFile()`), `store` (SQLite; `user.go` — `UserFromInput`; `check.go` — `WaitFirstCheck`; `heartbeat.go` — `Heartbeat`/`RunHeartbeat`/`Services`/`ServiceAlive`, имена `Service*`), `media` (`Meta` ↔ `meta.json` в папке видео: `Scan` по плейлисту, `Write` атомарно, `Read`), `config` (`Load(path, Validator)`, `ValidateURL`), `svc` (`Logger`, `SignalContext`), `ctxutil.Sleep`, `statusapi` (клиент status API для recorder и api: `Client.Fetch` → `Room{Status, User, HLSURL}` и HTTP-статус, таймаут 30 с, тело ошибки обрезается; `StatusError` + `RetryAfter`; `NewHTTPClient` с прокси и выключенным TLS-verify) |
| `recorder` | `internal/app` (`Run`: ClearStates → RecoverPartial → цикл `Sync` раз в 3 с, heartbeat с тем же периодом (`RunHeartbeat`), prune событий раз в час), `internal/recorder` (`Manager.Sync(ctx, desired)`, `watch`, `record`, `classifyFailure` в `delay.go`: 404/410 → неподтверждённого сразу `reject` (`user_rejected`), подтверждённого (`verified`) — `RoomNotFound` и опрос раз в сутки, после `notFoundLimit` = 3 подряд (счётчик в памяти) → `remove`; 401/403/451 → `RoomBlocked` и опрос раз в сутки, 5xx/сеть → `techFailure`, 429 → `Retry-After` (`StatusError`) или `rateLimited`, прочие 4xx → `disable`; события `user_disabled/removed/rejected` — только если база изменилась (`DisableUser` возвращает `changed`, `RemoveUser` — `ErrUserNotFound`): `Sync` может перезапустить слежение, выключившее себя между чтением списка и `forget`; `record` возвращает `recordSkipped/Done/Short/Stale/Failed`: после долгой записи (`longRecording`; длительность — `hls.Result.Active`, до последнего записанного сегмента, без ожидания застоя) опрос сразу, после неудачной — `recordRetryDelay` (15 с ×2 до 15 мин); `watch` хранит `lastSegment` (`hls.Result.Last`, сбрасывается при `hls_source == ""`) и передаёт его в `hls.Options.After`: запись продолжается после него, застывший плейлист → `ErrNoNewSegments` → `recordStale` без файла и событий; `recording_started` и `recording = true` — из `hls.Options.OnStart`, при первом записанном сегменте; `Sink` = store; status API — `core/statusapi`; `delay.go` интервалы), `internal/hls` (свой HLS-рекордер: `playlist.go`, `crypto.go` AES-128, `writer.go` part-файлы `.download` с правами `0o600` и ротацией раз в час, `RecoverPartial`, `recorder.go` `Record`/`DetectFormat`: таймауты запроса `playlistTimeout`/`segmentTimeout`, сегменты строго по порядку — неудачный повторяется, следующие ждут, после `maxSegmentAttempts` = 3 пропускается; идентичность сегмента — `SegmentKey` (только путь URL: без хоста — `hls_source` каждый раз ведёт на другой edge, — query и fragment) и между перезагрузками, и между записями (`Options.After` → `resumeAfter`); остановка ctx во время загрузки мастер-плейлиста — не ошибка (`recording_failed` не пишется); ни одного сегмента → `ErrNoSegments`), `internal/config`; `cmd/hlsprobe` — диагностика status API и CDN (ответы с замаскированными ссылками — `mask.go` — в `hlsprobe-<время>/`, см. README) |
| `tbot` | `internal/telegram` (`Bot`: `/add /del /list` — имя или ссылка через `store.UserFromInput`, ответ с итоговым именем; inline-меню удаления, `/add` ждёт `WaitFirstCheck` и отвечает `addReply`, поэтому `WithWorkers(4)`; `ForwardEvents` раз в 3 с с курсором `tbot` — в чат только `forwardKinds` (сейчас `user_disabled`, `user_removed`; о записях бот не пишет намеренно), `formatEntry`, `chunk`), `internal/app` (+ heartbeat `tbot` раз в 10 с), `internal/config` |
| `api` | `internal/httpapi` (fiber v3: `server.go` маршруты и middleware, подставляющий контекст `Serve` в `c.Context()` (в fiber v3 он по умолчанию `Background` и остановку службы не видит — `POST /watchlist` ждал бы recorder дольше `shutdownTimeout`; при остановке отвечает 503), `auth.go` bearer, `videos.go`, `watchlist.go` `/watchlist` (POST принимает ссылку, ждёт `WaitFirstCheck`, отвечает `{message, user, check}` или 404 для отклонённого, `check` = `checkNames[Outcome]`, при `CheckNoRecorder` — `recorder_offline` без ожидания; DELETE — через `store.UserFromInput`, 400 на недопустимое имя) + `/events`, `health.go` `GET /health` — `store.Services` с `Alive` и версия api, `live.go` `GET /watchlist/:user/live` (только пользователь из списка — `store.HasUser`; свежий `hls_source` через `LiveFetcher` = `statusapi.Client`, не `public` → 409, nil fetcher — нет `status_base_url` в api.json → 503), `reqlog.go` — лог каждого запроса через slog, подключается только при `-debug`), `internal/auth` (bcrypt + один uuid-токен в auth.json; запись атомарная через `.tmp` + rename, новый токен подменяет старый в памяти только после успешной записи), `internal/library` (`MediaInfo.Meta()`: сначала `meta.json` от converter (`core/media.Read`), иначе по m3u8: длительность — сумма `#EXTINF`; разрешение — ffprobe первого сегмента с таймаутом 10 с, один раз на видео: `resolutionCache` по пути, mtime и размеру плейлиста, `probeResolution` подменяется в тестах), `internal/app` (+ heartbeat `api` раз в 10 с; `Run` получает версию сборки), `internal/config`; `cmd/api hash-password` |
| `converter` | `internal/converter` (`Run` каждые `-interval`, по умолчанию 5 мин, в `dev.sh` 1 мин; `RunOnce`: `cleanupTemp` (готовую временную папку без исходника публикует — служба упала между удалением исходника и публикацией; остальные удаляет) → `pending` → `remux` в `media/.convert-<имя>` → `validatePlaylist` → `writeMeta` (`core/media.Scan` + разрешение ffprobe'ом из исходника, `FFprobe` пустой — без разрешения; ошибка только в лог, api посчитает сам) → удалить исходник → rename (`publish`) / `recordings/failed/`), `cmd/converter` (flock `data/converter.lock`, `-ffprobe` или ищется рядом с ffmpeg / в PATH; базу открывает только ради heartbeat `converter` раз в 10 с, в `-once` не пишет) |

## Общая база (`core/store`, `~/.cbr/data/cbr.db`)

- Таблицы: `watchlist(user, added_at, added_by, enabled, disabled_reason, verified)`, `watch_state(user, room_status, recording, updated_at)`,
  `events(id, at, kind, user, message)`, `cursors(consumer, event_id)`, `services(name, version, interval, seen_at)`. Время — unix ms.
- Миграции — срез `migrations` по `PRAGMA user_version`: только дописывать в конец. База новее бинарника
  (`user_version` больше числа миграций) → `Open` возвращает `ErrSchemaTooNew`, служба не стартует: частичный деплой виден сразу.
- `services` (миграция 4) — heartbeat: каждая служба через `RunHeartbeat` пишет имя (`store.Service*`), версию и период
  (`interval`), при корректной остановке удаляет строку. `ServiceState.Alive` — не старше 3 × `interval`. recorder бьётся
  раз в такт `Sync` (3 с), остальные — `DefaultHeartbeatInterval` (10 с). Читают api (`GET /health`) и `WaitFirstCheck`.
- Кто пишет: watchlist — tbot/api (`AddUser/RemoveUser`) и recorder (`DisableUser`, `RemoveUser` при 404/410, `MarkVerified` при первом успешном ответе status API); watch_state и events — только recorder (`RemoveUser` удаляет и строку watch_state, иначе повторное добавление при выключенном recorder выглядело бы проверенным).
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
  опрос базы раз в 0,5 с); без живого heartbeat recorder'а сразу `CheckNoRecorder` (api: `check: "recorder_offline"`,
  tbot: «recorder is not running», iOS: уведомление под полем). Признак проверки — строка в watch_state, выключение или удаление пользователя, поэтому recorder
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
  (5,1 Мбит/с), звук — отдельные дорожки `#EXT-X-MEDIA`, варианты — только видео. Recorder пишет только лучший
  вариант: записи ноября 2026 в dev — без звука, записи 2025 года — со звуком. Запись отдельной дорожки
  пробовали и убрали (решение владельца);
- LL-HLS (`#EXT-X-PART`, `CAN-BLOCK-RELOAD=YES`), fMP4 (`#EXT-X-MAP`, `.m4s`), target 2 с, сегменты по 1,6 с,
  в плейлисте всего 4 сегмента (окно 6,4 с), видеосегмент 1080p ~1 МБ качается 0,7–1 с; без шифрования;
- звук: у каждого варианта `AUDIO="<группа>"` (1080p — `audio_aac_128`, остальные — `audio_aac_96`), дорожка — свой
  media-плейлист со своим init; номера сегментов (`MEDIA-SEQUENCE`) у звука и видео общие, но звук сегмента N
  начинается ~0,9 с раньше видео того же N (`PROGRAM-DATE-TIME`, tfdt): склеивать дорожки по номеру нельзя, только
  по времени начала (`ffprobe` + `-itsoffset`), и в MPEG-TS — в fMP4 ffmpeg переносит смещение в edit list
  init-сегмента, и оно теряется; звуковой сегмент ~25 КБ;
- токен `session` за 5 минут перезагрузок не менялся и не истёк: сравнение сегментов по пути URL работает
  (по полному URL — нет: лог dev 2026-11-09 показал смену edge-хоста между попытками одного стрима),
  переходить на `MEDIA-SEQUENCE` не нужно.

Не проверено: коды для заблокированного пользователя (401/403), `Retry-After` на 429, срок жизни `session` дольше 5 минут.

## Соглашения

- Службы не импортируют друг друга и не общаются по сети между собой — только `core/store` и файлы в `~/.cbr`.
  Внешний status API опрашивают recorder (постоянно) и api (по запросу живого просмотра) — общий клиент в `core/statusapi`.
- Новый интерфейс управления = новый модуль: пишет `watchlist`, читает `events` со своим `Consumer` в `cursors`.
- У службы свой `~/.cbr/config/<служба>.json` (`config.example.json` в папке модуля), бинарник `cmd/<служба>`,
  unit `cbr-<служба>.service`, лог `logs/<служба>.log`. Новую службу добавить в `deploy/install.sh` (`ALL_SERVICES`,
  `preflight`), `dev.sh` (`ALL_SERVICES`, `ready`, `args`), `Makefile` (`MODULES`) и бинарники в корневой `.gitignore`.
- `log/slog`, остановка через `context`, без глобального состояния; комментарии и документация на русском;
  тесты рядом с кодом, база в тестах — настоящая SQLite во временной папке.
- Схема базы и протокол «api/tbot ждут recorder» общие для всех служб — на сервере обновлять службы вместе
  (`install.sh` без аргументов). Старый бинарник с новой базой не стартует (`ErrSchemaTooNew`); новый api/tbot со
  старым recorder (без heartbeat) считают его выключенным и отвечают на добавление сразу `recorder_offline`.
- SQLite в WAL с несколькими процессами требует локального диска: `~/.cbr` на NFS/SMB ломает блокировки и согласованность.
- Изменил JSON `/videos`, `/watchlist` или ответ `POST /watchlist` → обнови `apps/ios/CBRPlayer/Models/` и таблицу в `api/README.md`.
  Поля `meta.json` — это поля `/videos`: менять `core/media.Meta` и `MediaFile` вместе.
- Права: служебная папка и подпапки `0o750` (`home.Ensure`, `chmod 750` в `dev.sh`/`install.sh`), секреты и записи `0o600`.
  Все службы работают от одного пользователя (`User=` в unit), поэтому шире права не нужны.
- Зависимости: при `go mod tidy` следи, чтобы версии не откатывались (fiber v3.3.0, go-telegram/bot v1.20.0,
  modernc.org/sqlite v1.61.1). `go mod verify` ругается на локальный `cbr/core` (replace) — это нормально.
- Уязвимость в косвенной зависимости (govulncheck в `make check`) — точечно `go get <модуль>@<fixed>` в нужном модуле,
  без `go get -u ./...`: иначе сдвинутся закреплённые версии. Так обновлены brotli 1.2.7 и klauspost/compress 1.20.2 в api.
