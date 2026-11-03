#!/bin/bash
# Сборка и (пере)установка cbrconvert как отдельной systemd-службы на Ubuntu.
# cbrconvert переносит завершённые записи из ~/.cbr/recordings в медиатеку
# ~/.cbr/media в виде HLS без перекодирования (ffmpeg -c copy) и удаляет исходники.
# Логи пишутся в ~/.cbr/logs/cbrconvert.log; их ротацию покрывает правило
# /etc/logrotate.d/cbrservice (ставится deploy/cbrservice.sh, маска logs/*.log).
#
# Параметры службы можно задать переменными при запуске скрипта:
#   CONVERT_INTERVAL=5m CONVERT_MIN_AGE=1m deploy/cbrconvert.sh

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$DEPLOY_DIR/.." && pwd)"
CBR_HOME="${CBR_HOME:-$HOME/.cbr}"
BIN="$CBR_HOME/bin/cbrconvert"
SERVICE_NAME="cbrconvert.service"
SERVICE_FILE="/etc/systemd/system/$SERVICE_NAME"
# Пользователь службы. Не $USER: он не задан при запуске из cron/CI/docker exec,
# и пустой User= молча запустил бы службу от root.
RUN_USER="$(id -un)"
CONVERT_INTERVAL="${CONVERT_INTERVAL:-5m}"
CONVERT_MIN_AGE="${CONVERT_MIN_AGE:-1m}"

if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "❌ ffmpeg не установлен: sudo apt install ffmpeg"
  exit 1
fi
FFMPEG="$(command -v ffmpeg)"

echo "Переход в папку проекта..."
cd "$PROJECT_DIR" || { echo "Не удалось перейти в $PROJECT_DIR"; exit 1; }

echo "Обновление репозитория..."
git pull || { echo "Не удалось выполнить git pull"; exit 1; }

echo "Сборка cbrconvert..."
BUILD_OUTPUT="$(mktemp)"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$BUILD_OUTPUT" ./cmd/cbrconvert \
  || { echo "Ошибка при сборке (нужен Go 1.26+)"; exit 1; }
echo "Собрана версия $VERSION"

if systemctl is-active --quiet "$SERVICE_NAME"; then
  echo "Остановка службы $SERVICE_NAME..."
  sudo systemctl stop "$SERVICE_NAME" || { echo "Не удалось остановить службу $SERVICE_NAME"; exit 1; }
fi

echo "Подготовка служебной папки $CBR_HOME..."
mkdir -p "$CBR_HOME"/{bin,data,recordings,media,logs} || { echo "Не удалось создать $CBR_HOME"; exit 1; }

echo "Установка бинарника в $BIN..."
install -m 0755 "$BUILD_OUTPUT" "$BIN" || { echo "Ошибка при копировании бинарника"; exit 1; }
rm -f "$BUILD_OUTPUT"

# Unit-файл перезаписывается при каждом деплое, чтобы изменения в нём применялись.
# Nice/IOSchedulingClass: переупаковка — в основном диск, она не должна мешать
# записи стримов и раздаче видео.
echo "Запись файла службы $SERVICE_FILE..."
sudo tee "$SERVICE_FILE" >/dev/null <<EOF || { echo "Не удалось записать $SERVICE_FILE"; exit 1; }
[Unit]
Description=CBR Convert (recordings -> HLS media library)
After=local-fs.target

[Service]
User=$RUN_USER
Environment=CBR_HOME=$CBR_HOME
WorkingDirectory=$CBR_HOME
ExecStart=$BIN -interval $CONVERT_INTERVAL -min-age $CONVERT_MIN_AGE -ffmpeg $FFMPEG
Restart=always
RestartSec=10
Nice=10
IOSchedulingClass=idle
StandardOutput=append:$CBR_HOME/logs/cbrconvert.log
StandardError=append:$CBR_HOME/logs/cbrconvert.log

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload || { echo "Ошибка при перезагрузке systemd"; exit 1; }
sudo systemctl enable "$SERVICE_NAME" || { echo "Не удалось включить службу $SERVICE_NAME"; exit 1; }

echo "Запуск службы $SERVICE_NAME..."
sudo systemctl start "$SERVICE_NAME" || { echo "Не удалось запустить службу $SERVICE_NAME"; exit 1; }

echo "cbrconvert установлен и запущен. Логи: $CBR_HOME/logs/cbrconvert.log"
