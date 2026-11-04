# converter

Служба переноса записей [recorder](../recorder/README.md) из `~/.cbr/recordings/` в медиатеку
`~/.cbr/media/` в виде HLS без перекодирования (`ffmpeg -c copy`); исходники удаляются. С остальными
службами связана только файлами в `~/.cbr`, базу не использует. Нужен `ffmpeg` (`sudo apt install ffmpeg`).

## Как работает

Каждые 5 минут (`-interval`) converter ищет в `recordings/` завершённые записи — файлы `.ts`/`.mp4`
(пока part пишется, у него суффикс `.download`) — и для каждой:

1. нарезает её в HLS без перекодирования во временную скрытую папку `media/.convert-<имя>`:
   `.ts` → MPEG-TS сегменты, `.mp4` (fMP4) → fMP4 сегменты `.m4s` с `init.mp4`, примерно по 6 секунд
   (резать можно только по ключевым кадрам);
2. проверяет, что плейлист дописан до `#EXT-X-ENDLIST`, и атомарно переименовывает папку в `media/<имя>`
   (плеер не видит недорезанное видео; существующие видео не перезаписываются — к имени добавляется `_2`, `_3`…);
3. удаляет исходник.

Каждый part — отдельное видео. Если ffmpeg не справился, запись переносится в `recordings/failed/` и больше
не обрабатывается. Остановка службы прерывает текущую нарезку; исходник остаётся и будет обработан при
следующем запуске. Второй экземпляр над той же папкой не запустится (блокировка `data/converter.lock`).

```bash
~/.cbr/bin/converter -once                       # один проход и выход
~/.cbr/bin/converter -interval 5m -min-age 1m    # как служба
```

Флаги: `-interval`, `-min-age` (пропускать файлы, изменённые недавно), `-ffmpeg` (путь), `-once`, `-debug`, `-version`.

## Запуск

```bash
make run                                                  # один проход над $CBR_HOME (-once -debug)
services/deploy/install.sh converter                      # на сервере: unit cbr-converter.service
CONVERT_INTERVAL=10m services/deploy/install.sh converter # свои -interval / -min-age
```

Unit работает с пониженным приоритетом CPU и диска (`Nice=10`, `IOSchedulingClass=idle`), чтобы не мешать записи.
