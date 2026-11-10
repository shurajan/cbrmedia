# api

HTTP API для плеера CBRPlayer: авторизация по паролю, раздача видео из медиатеки `~/.cbr/media`
и управление списком слежения через общую базу `~/.cbr/data/cbr.db` (вкладка «Watchlist» в iOS).

## Эндпоинты

Все, кроме `/auth`, требуют `Authorization: Bearer <token>`. Действует один токен: новый вход отменяет старый.

| Метод и путь | Ответ |
|---|---|
| `POST /auth` `{"password"}` | `{"token"}`; ошибки — `{"error"}`, как у остальных маршрутов: 400 `invalid json`, 401 `invalid password`, 500 `failed to issue token` |
| `GET /videos` | `[{id, name, hlsURL, createdAt?, duration, resolution?, sizeMB?, segmentCount, avgSegmentDuration}]`; метаданные — из `meta.json`, который converter кладёт рядом с плейлистом (старым видео дописывает при очередном проходе); пока файла нет, `duration`, `segmentCount` и прочие — нули |
| `GET /videos/:name/*` | файлы HLS (`playlist.m3u8`, `.ts`, `.m4s`, `init.mp4`…) |
| `DELETE /videos/:name` | удалить видео целиком |
| `GET /watchlist` | `[{user, addedAt, addedBy, enabled, disabledReason?, roomStatus?, recording, updatedAt?}]` |
| `POST /watchlist` `{"user"}` | Отвечает после первой проверки пользователя recorder'ом (обычно 5–8 с, не дольше 30 с): 201 `{message, user, check, roomStatus?, reason?}`, `check` — `watching`, `blocked`, `not_found`, `disabled`, `pending` (recorder не успел) или `recorder_offline` (recorder не запущен — ответ сразу, без ожидания; проверит при запуске); 404, если пользователь не найден — в список он не добавлен; 409 если уже есть; 400 при недопустимом имени. Выключенного включает снова. В `user` можно передать ссылку на страницу — сохраняется только имя (последний сегмент пути) |
| `DELETE /watchlist/:user` | 200; 404 если нет; имя в любом регистре (приводится к нижнему), недопустимое — 400 |
| `GET /watchlist/:user/live` | Свежая ссылка на живой стрим — api в момент запроса спрашивает status API: 200 `{user, roomStatus, hlsURL}` (ссылка на CDN с токеном, клиент открывает её напрямую, без Bearer); 409 `{error, roomStatus}`, если стрим не `public`; 404, если пользователя нет в списке или status API его не знает; 502 при ошибке status API; 503, если в `api.json` нет `status_base_url` |
| `GET /events?after=ID&limit=N` | `[{id, at, kind, user?, message}]` с `id > after`, `limit` по умолчанию 100, максимум 500 |
| `GET /health` | `{version, services: [{name, version, alive, seenAt, interval}]}` — версия api и heartbeat всех запущенных служб из базы: `alive` — отметка не старше 3 × `interval` (секунды); корректно остановленная служба в списке отсутствует, упавшая — есть с `alive: false` |

`updatedAt` пуст, если recorder сейчас за пользователем не следит (выключен или ещё не подхватил).
Время — RFC 3339 в UTC.

## Настройка

```bash
~/.cbr/bin/api hash-password --password <пароль>   # создать ~/.cbr/config/auth.json
```

Пароль задаётся при установке и меняется редко. Если всё же менять при работающей службе — после
`hash-password` перезапустить api (`systemctl restart cbr-api`): служба читает `auth.json` один раз при
старте и при каждом входе записывает его целиком из памяти, так что без перезапуска вход старым паролем
вернёт в файл старый хэш.

`~/.cbr/config/api.json` необязателен (шаблон — `config.example.json`):

- `addr` — адрес, по умолчанию `:8000`;
- `status_base_url` — базовый адрес status API, тот же, что `base_url` в `recorder.json`; без него
  `GET /watchlist/:user/live` отвечает 503 (живой просмотр выключен), остальное работает;
- `proxy` — HTTP-прокси для запросов к status API, как в `recorder.json` (необязательно).
  Проверка TLS-сертификатов status API отключена, как в recorder.

## Запуск

```bash
make run                        # локально, с -debug (в логе каждый запрос)
services/deploy/install.sh api  # на сервере: unit cbr-api.service
```

Флаги: `-debug` (подробные логи, включая каждый HTTP-запрос — на сервере не включать: плеер запрашивает
сегменты каждые несколько секунд), `-version`.
