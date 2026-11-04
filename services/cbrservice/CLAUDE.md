# cbrservice (Go)

Полное описание для людей — `README.md` (служебная папка, деплой) и `docs/watcher.md` (поля config.json,
логика опроса). Здесь — карта кода для доработок.

## Команды (из этой папки)

```bash
make test    # go test -race ./...
make check   # vet + golangci-lint + test + govulncheck — прогонять перед коммитом
make build   # bin/cbrservice, bin/cbrconvert
make fmt     # gofmt + goimports через golangci-lint
CBR_HOME=/tmp/cbr-dev ./start.sh   # локальный запуск в изолированной папке
```

Go 1.26+ (toolchain закреплён в go.mod). Зависимости: fiber/v3, go-telegram/bot, bbolt, grafov/m3u8, bcrypt, uuid.

## Два бинарника

- `cmd/cbrservice` — флаги `-log -debug -version`, подкоманда `hash-password --password X`.
  `main` → `prepareHome()` (resolve + mkdir + chdir в `~/.cbr`) → `app.Run`.
- `cmd/cbrconvert` — флаги `-once -interval -min-age -ffmpeg`; `internal/converter`, блокировка `data/cbrconvert.lock`.
  Независимая systemd-служба, с cbrservice общается только через файловую систему.

## Пакеты `internal/`

| Пакет | Ключевое |
|-------|----------|
| `app` | `Run(ctx, layout, opts, log)`: собирает всё, ждёт ctx, первая ошибка компонента отменяет остальные (`context.WithCancelCause`). `telegramOptions` подменяются в тестах |
| `home` | `Layout{Root, Bin, Config, Data, Recordings, Media, Logs}`; `CBR_HOME` переопределяет `~/.cbr`. Все пути — только отсюда |
| `config` | `Config{BaseURL, Storage.DB, Poller.Proxy, Telegram{BotToken, AllowedUserID, Proxy}, HTTP.Addr}`; `Load(path, dataDir)` валидирует, `:8000` по умолчанию |
| `auth` | `auth.json`: bcrypt-хэш + один токен (uuid). `CheckPassword`, `IssueToken`, `ValidToken` (constant-time) |
| `httpapi` | fiber v3. `server.go` маршруты, `auth.go` (`authHandler`, `bearerAuth`), `videos.go` (`listVideos`, `streamFile` с защитой от path traversal, `deleteVideo`, `contentTypes`) |
| `library` | `MediaInfo` (папка видео: `ID()`=simpleID пути, `StreamURL()`, `CreatedAt()`), `Playlist` (длительность/разрешение/размер из m3u8, кэш, fallback на ffprobe) |
| `recorder` | `Manager` (Watch/Unwatch/Watching, `Notifications()` → Telegram), `watch.go` цикл опроса, `status.go` `StatusClient.Fetch` + `NewHTTPClient` (TLS verify выключен, прокси), `delay.go` интервалы по статусу (15s/1m/15m) |
| `hls` | собственный HLS-рекордер без ffmpeg: `playlist.go` парсинг/выбор варианта, `crypto.go` AES-128, `writer.go` part-файлы `.download` + ротация раз в час, `RecoverPartial` при старте, `recorder.go` `Record`/`DetectFormat` (ts vs fMP4), stall-детект |
| `storage` | bbolt, список пользователей: `Add/Remove/List` |
| `telegram` | бот: `/add`, `/del` (inline-меню), `/list`; доступ только `AllowedUserID`. Интерфейсы `UserStore`, `Watcher` |
| `converter` | recordings → `media/.convert-<имя>` → проверка `#EXT-X-ENDLIST` → атомарный rename; ошибки → `recordings/failed/` |
| `ctxutil` | `Sleep(ctx, d)` |

## Соглашения

- Зависимости сверху вниз: `cmd` → `app` → компоненты; компоненты не импортируют друг друга, связь через интерфейсы.
- Без глобального состояния; логирование `log/slog` с `component`; остановка через `context`.
- Doc-комментарии и сообщения — на русском; тесты рядом с кодом (`*_test.go`), HTTP-тесты через `helpers_test.go`.
- Изменил JSON `/videos` → обнови `apps/ios/CBRPlayer/Models/VideoItemModel.swift`.
- Деплой: `deploy/cbrservice.sh`, `deploy/cbrconvert.sh` (git pull + build + systemd unit + logrotate) — запускаются на Ubuntu-сервере, не локально.
