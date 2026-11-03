# cbrservice

Бэкенд для Ubuntu из двух независимых systemd-служб над общей папкой `~/.cbr`.

**cbrservice** — основной процесс:

- **recorder**: опрашивает status API по списку пользователей и, когда стрим начинается,
  записывает его (HLS, без ffmpeg) в `~/.cbr/recordings/`.
- **telegram**: бот для управления списком пользователей (`/add`, `/del`, `/list`) и уведомлений.
- **httpapi**: HTTP API на `:8000` для клиента CBRPlayer: авторизация и раздача видео из `~/.cbr/media/`.

**cbrconvert** — отдельная служба: переносит завершённые записи из `~/.cbr/recordings/` в `~/.cbr/media/`
в виде HLS без перекодирования (`ffmpeg -c copy`) и удаляет исходники. Подробнее — в разделе
[Перенос записей в медиатеку](#перенос-записей-в-медиатеку-cbrconvert).

Формат `config.json` описан в [docs/watcher.md](docs/watcher.md). Требуется **Go 1.26+**; нужная патч-версия
закреплена в `go.mod` (`toolchain`), Go скачает её сам.

## Структура

```
cbrservice/
├── cmd/cbrservice/     # main: флаги, сигналы, подкоманда hash-password
├── cmd/cbrconvert/     # main службы переноса записей в медиатеку
├── internal/
│   ├── app/            # сборка компонентов и общий жизненный цикл
│   ├── config/         # типизированный config.json с проверкой при старте
│   ├── home/           # служебная папка ~/.cbr: все пути сервиса
│   ├── storage/        # список пользователей (bbolt)
│   ├── recorder/       # status API, цикл опроса, менеджер слежения
│   ├── hls/            # запись живого HLS: плейлисты, AES-128, part-файлы
│   ├── telegram/       # Telegram-бот
│   ├── httpapi/        # HTTP API для плеера (fiber)
│   ├── auth/           # пароль и токен плеера (auth.json)
│   ├── library/        # медиатека: метаданные видео
│   ├── converter/      # recordings → HLS в media (ffmpeg -c copy)
│   └── ctxutil/        # мелкие хелперы для context
├── deploy/             # установка на Ubuntu: скрипт, шаблон config.json, правила logrotate
├── scripts/            # утилиты ffmpeg/HLS (make_hls.sh, convert.sh, …)
├── postman/            # коллекция запросов к API
├── docs/
├── Makefile
└── .golangci.yml
```

Зависимости идут сверху вниз: `cmd` → `app` → компоненты. Компоненты не знают друг о друге и общаются через
интерфейсы (`telegram.Watcher`, `recorder.StatusFetcher` и т.д.). Глобального состояния нет, логирование
через `log/slog`, остановка через `context`.

## Разработка

```bash
make help     # список целей
make build    # bin/cbrservice и bin/cbrconvert с версией из git describe
make test     # тесты с -race
make lint     # golangci-lint (конфиг .golangci.yml)
make vuln     # govulncheck
make check    # vet + lint + test + vuln: всё перед коммитом
```

Флаги: `-log` логирует HTTP-запросы, `-debug` включает подробные логи, `-version` печатает версию.

## Служебная папка `~/.cbr`

Всё, что нужно сервису для работы, лежит в одной папке, `~/.cbr`. Её можно переопределить переменной `CBR_HOME`.
Сервис сам создаёт недостающие директории и делает `~/.cbr` своей рабочей папкой, поэтому неважно, откуда запущен бинарник.
Пути описаны в одном месте: `internal/home`.

```
~/.cbr/
├── bin/                    # бинарники cbrservice и cbrconvert
├── config/
│   ├── config.json         # настройки: токен бота, base_url, прокси, адрес HTTP (chmod 600)
│   └── auth.json           # хэш пароля для плеера (создаётся командой hash-password)
├── data/data.db            # список отслеживаемых пользователей (bbolt)
├── recordings/             # сырые записи стримов (*.ts / *.mp4, part-файлы)
│   └── failed/             # записи, которые cbrconvert не смог преобразовать
├── media/                  # HLS-библиотека, которую видит плеер (<папка>/playlist.m3u8)
└── logs/                   # cbrservice.log, cbrconvert.log (ротация через logrotate)
```

Относительный путь `storage.db` в `config.json` разрешается от `~/.cbr/data/`.
Шаблон конфига лежит в [deploy/config.example.json](deploy/config.example.json), описание полей в [docs/watcher.md](docs/watcher.md).

`recordings/` и `media/` разделены: запись попадает в плеер после переноса службой cbrconvert.

## Перенос записей в медиатеку (cbrconvert)

Каждые 5 минут (`-interval`) cbrconvert ищет в `recordings/` завершённые записи — файлы `.ts`/`.mp4`
(пока part пишется, у него суффикс `.download`) — и для каждой:

1. нарезает её в HLS без перекодирования во временную скрытую папку `media/.convert-<имя>`:
   `.ts` → MPEG-TS сегменты, `.mp4` (fMP4) → fMP4 сегменты `.m4s` с `init.mp4`, примерно по 6 секунд
   (резать можно только по ключевым кадрам);
2. проверяет, что плейлист дописан до `#EXT-X-ENDLIST`, и атомарно переименовывает папку в `media/<имя>`
   (плеер не видит недорезанное видео; существующие видео не перезаписываются — к имени добавляется `_2`, `_3`…);
3. удаляет исходник.

Каждый part — отдельное видео. Если ffmpeg не справился, запись переносится в `recordings/failed/` и больше
не обрабатывается. Остановка службы прерывает текущую нарезку; исходник остаётся и будет обработан при
следующем запуске. Второй экземпляр над той же папкой не запустится (блокировка `data/cbrconvert.lock`).

```bash
~/.cbr/bin/cbrconvert -once                       # один проход и выход
~/.cbr/bin/cbrconvert -interval 5m -min-age 1m    # как служба
```

Нужен `ffmpeg` (`sudo apt install ffmpeg`). Установка службы — `deploy/cbrconvert.sh` (см. ниже).

## Запуск локально

```bash
./start.sh            # соберёт ~/.cbr/bin/cbrservice; при первом запуске создаст шаблон config.json
~/.cbr/bin/cbrservice hash-password --password <пароль>
./start.sh [-log]     # -log включает логирование HTTP-запросов
```

Для отдельной тестовой папки используйте `CBR_HOME=/tmp/cbr-dev ./start.sh`.

## Деплой на Ubuntu

```bash
services/cbrservice/deploy/cbrservice.sh
```

Что делает скрипт:
1. `git pull` и сборка.
2. Кладёт бинарник в `~/.cbr/bin/`. Если `config.json` или `auth.json` нет, подсказывает, как их создать, и останавливается.
3. Записывает `/etc/systemd/system/cbrservice.service` (`WorkingDirectory=~/.cbr`, логи в `~/.cbr/logs/`).
4. Устанавливает ротацию логов в `/etc/logrotate.d/cbrservice` (шаблон `deploy/logrotate.conf`):
   ротация раз в день или при размере больше 100 МБ, хранятся 14 архивов в gzip, используется `copytruncate`, поэтому перезапуск службы не нужен.
5. Запускает службу.

Посмотреть логи: `tail -f ~/.cbr/logs/cbrservice.log`.

Служба переноса записей ставится отдельно (нужен `ffmpeg`):

```bash
services/cbrservice/deploy/cbrconvert.sh                         # интервал 5m, min-age 1m
CONVERT_INTERVAL=10m services/cbrservice/deploy/cbrconvert.sh   # свои параметры
```

Скрипт собирает `~/.cbr/bin/cbrconvert`, записывает `/etc/systemd/system/cbrconvert.service`
(с пониженным приоритетом CPU и диска, чтобы не мешать записи) и запускает её. Логи: `~/.cbr/logs/cbrconvert.log`,
их ротацию покрывает то же правило logrotate. Службы независимы: cbrconvert можно останавливать и
перезапускать, не трогая cbrservice.

## Тесты

```bash
go test ./...
```
