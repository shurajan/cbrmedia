# recorder

Служба записи стримов. Берёт список слежения из общей базы `~/.cbr/data/cbr.db`, опрашивает status API
по каждому включённому пользователю и, когда стрим начинается, записывает его как HLS без ffmpeg
в `~/.cbr/recordings/`. Состояние (статус комнаты, идёт ли запись) и события пишет обратно в базу —
их показывают интерфейсы управления ([tbot](../tbot/README.md), [api](../api/README.md)).

## Legal Notice

This software is provided for recording streams that you own or that you are expressly authorized to record. You are solely responsible for ensuring that your use of this software complies with all applicable copyright and digital rights legislation — including, where applicable, the Digital Millennium Copyright Act (DMCA), the EU Directive on Copyright in the Digital Single Market, and equivalent laws in your jurisdiction — as well as the terms of service of any platform from which content is accessed. Do not use this software to record, store, or distribute content without the consent of the rights holder. The authors and contributors accept no liability for any unauthorized or unlawful use.

## Как работает

- **Сверка со списком.** Каждые 3 секунды recorder читает включённых пользователей из `watchlist` и
  запускает или останавливает слежения. Команд он не получает: чтобы начать запись, достаточно добавить
  пользователя в базу (через tbot, api или любой другой интерфейс).
- **Опрос.** Интервал зависит от статуса комнаты: 15 секунд, если стрим публичный; 1 минута, если
  пользователь онлайн, но стрим недоступен (private, away, hidden, password protected); 15 минут, если офлайн.
  На 5xx и сетевые ошибки — пауза 20 минут и повтор.
- **4xx от status API** выключает пользователя в базе (`enabled = 0`, с причиной) и пишет событие
  `user_disabled`. Повторное добавление пользователя включает его снова.
- **Запись HLS.** Выбирает вариант с максимальным битрейтом из master-плейлиста, непрерывно скачивает
  новые сегменты, расшифровывает AES-128, поддерживает fMP4/CMAF с init-сегментами (part никогда не
  содержит фрагменты без init; новый init начинает новый part). Запись заканчивается на `#EXT-X-ENDLIST`,
  при ответе 4xx на плейлист или если 1 минуту (или 6× target duration, что больше) не удалось записать
  новый сегмент — это ловит CDN, которые продолжают отдавать застывший плейлист после конца эфира.
- **Файлы.** `<user>_<дата>_part<N>.ts` или `.mp4` в зависимости от формата; part ротируется после каждого
  часа записанного контента. Пока part пишется, у него суффикс `.download`; незавершённые после падения
  part'ы восстанавливаются при старте. Существующие файлы не перезаписываются.
- **События** в журнал: `recording_started`, `recording_finished`, `recording_failed`, `user_disabled`.
  События старше 30 дней удаляются.
- **Проверка TLS-сертификатов отключена** для опроса и скачивания (при старте пишется предупреждение).

## Конфиг `~/.cbr/config/recorder.json`

```json
{
  "base_url": "https://example.com/api/streamcontext",
  "proxy": "http://127.0.0.1:8080"
}
```

- `base_url` — базовый адрес status API; пользователь опрашивается по `<base_url>/<user>/`, ответ содержит
  `room_status` и, если стрим идёт, `hls_source`.
- `proxy` (необязательно) — HTTP-прокси для опроса и скачивания.

Шаблон — [config.example.json](config.example.json). Ошибки в конфиге останавливают службу при старте.

## Запуск

```bash
make run                       # локально, данные в $CBR_HOME (по умолчанию ~/.cbr)
services/deploy/install.sh recorder   # на сервере: unit cbr-recorder.service
```

Флаги: `-debug`, `-version`.
