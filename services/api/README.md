# api

HTTP API для плеера CBRPlayer: авторизация по паролю, раздача видео из медиатеки `~/.cbr/media`
и управление списком слежения через общую базу `~/.cbr/data/cbr.db` (вкладка «Watchlist» в iOS).

## Эндпоинты

Все, кроме `/auth`, требуют `Authorization: Bearer <token>`. Действует один токен: новый вход отменяет старый.

| Метод и путь | Ответ |
|---|---|
| `POST /auth` `{"password"}` | `{"token"}`; 401 при неверном пароле |
| `GET /videos` | `[{id, name, hlsURL, createdAt?, duration, resolution?, sizeMB?, segmentCount, avgSegmentDuration}]` |
| `GET /videos/:name/*` | файлы HLS (`playlist.m3u8`, `.ts`, `.m4s`, `init.mp4`…) |
| `DELETE /videos/:name` | удалить видео целиком |
| `GET /watchlist` | `[{user, addedAt, addedBy, enabled, disabledReason?, roomStatus?, recording, updatedAt?}]` |
| `POST /watchlist` `{"user"}` | Отвечает после первой проверки пользователя recorder'ом (обычно 5–8 с, не дольше 30 с): 201 `{message, user, check, roomStatus?, reason?}`, `check` — `watching`, `blocked`, `not_found`, `disabled` или `pending` (recorder не успел); 404, если пользователь не найден — в список он не добавлен; 409 если уже есть; 400 при недопустимом имени. Выключенного включает снова. В `user` можно передать ссылку на страницу — сохраняется только имя (последний сегмент пути) |
| `DELETE /watchlist/:user` | 200; 404 если нет; имя в любом регистре (приводится к нижнему), недопустимое — 400 |
| `GET /events?after=ID&limit=N` | `[{id, at, kind, user?, message}]` с `id > after`, `limit` по умолчанию 100, максимум 500 |

`updatedAt` пуст, если recorder сейчас за пользователем не следит (выключен или ещё не подхватил).
Время — RFC 3339 в UTC.

## Настройка

```bash
~/.cbr/bin/api hash-password --password <пароль>   # создать ~/.cbr/config/auth.json
```

`~/.cbr/config/api.json` необязателен: `{"addr": ":8000"}` (по умолчанию `:8000`).

## Запуск

```bash
make run                        # локально, с -debug (в логе каждый запрос)
services/deploy/install.sh api  # на сервере: unit cbr-api.service
```

Флаги: `-debug` (подробные логи, включая каждый HTTP-запрос — на сервере не включать: плеер запрашивает
сегменты каждые несколько секунд), `-version`.
