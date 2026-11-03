#!/bin/bash
# Сборка и (пере)установка cbrservice как systemd-службы на Ubuntu.
# Всё, что нужно службе, лежит в служебной папке ~/.cbr (см. README):
#   bin/ config/ data/ recordings/ media/ logs/
# Также устанавливает ротацию логов в /etc/logrotate.d/cbrservice.

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$DEPLOY_DIR/.." && pwd)"
CBR_HOME="${CBR_HOME:-$HOME/.cbr}"
BIN="$CBR_HOME/bin/cbrservice"
SERVICE_NAME="cbrservice.service"
SERVICE_FILE="/etc/systemd/system/$SERVICE_NAME"
# Пользователь службы. Не $USER: он не задан при запуске из cron/CI/docker exec,
# и пустой User= молча запустил бы службу от root.
RUN_USER="$(id -un)"
LOGROTATE_FILE="/etc/logrotate.d/cbrservice"

echo "Переход в папку проекта..."
cd "$PROJECT_DIR" || { echo "Не удалось перейти в $PROJECT_DIR"; exit 1; }

echo "Обновление репозитория..."
git pull || { echo "Не удалось выполнить git pull"; exit 1; }

echo "Сборка проекта..."
BUILD_OUTPUT="$(mktemp)"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$BUILD_OUTPUT" ./cmd/cbrservice \
  || { echo "Ошибка при сборке проекта (нужен Go 1.26+)"; exit 1; }
echo "Собрана версия $VERSION"

if systemctl is-active --quiet "$SERVICE_NAME"; then
  echo "Остановка службы $SERVICE_NAME..."
  sudo systemctl stop "$SERVICE_NAME" || { echo "Не удалось остановить службу $SERVICE_NAME"; exit 1; }
fi

echo "Подготовка служебной папки $CBR_HOME..."
mkdir -p "$CBR_HOME"/{bin,config,data,recordings,media,logs} || { echo "Не удалось создать $CBR_HOME"; exit 1; }

echo "Установка бинарника в $BIN..."
install -m 0755 "$BUILD_OUTPUT" "$BIN" || { echo "Ошибка при копировании бинарника"; exit 1; }
rm -f "$BUILD_OUTPUT"

# Конфигурация: без неё служба не стартует
if [ ! -f "$CBR_HOME/config/config.json" ]; then
  cp "$DEPLOY_DIR/config.example.json" "$CBR_HOME/config/config.json"
  chmod 600 "$CBR_HOME/config/config.json"
  echo "⚠️  Создан шаблон $CBR_HOME/config/config.json — заполните его и запустите скрипт снова."
  exit 1
fi
if [ ! -f "$CBR_HOME/config/auth.json" ]; then
  echo "⚠️  Нет $CBR_HOME/config/auth.json. Задайте пароль для плеера и запустите скрипт снова:"
  echo "    CBR_HOME=$CBR_HOME $BIN hash-password --password <пароль>"
  exit 1
fi

# Unit-файл перезаписывается при каждом деплое, чтобы изменения в нём применялись
echo "Запись файла службы $SERVICE_FILE..."
sudo tee "$SERVICE_FILE" >/dev/null <<EOF || { echo "Не удалось записать $SERVICE_FILE"; exit 1; }
[Unit]
Description=CBR Service (stream recorder + media server)
After=network-online.target
Wants=network-online.target

[Service]
User=$RUN_USER
Environment=CBR_HOME=$CBR_HOME
WorkingDirectory=$CBR_HOME
ExecStart=$BIN
Restart=always
RestartSec=5
StandardOutput=append:$CBR_HOME/logs/cbrservice.log
StandardError=append:$CBR_HOME/logs/cbrservice.log

[Install]
WantedBy=multi-user.target
EOF

# Ротация логов (шаблон deploy/logrotate.conf)
echo "Запись правил ротации логов $LOGROTATE_FILE..."
sed -e "s|@CBR_HOME@|$CBR_HOME|g" -e "s|@USER@|$RUN_USER|g" -e "s|@GROUP@|$(id -gn)|g" \
  "$DEPLOY_DIR/logrotate.conf" | sudo tee "$LOGROTATE_FILE" >/dev/null \
  || { echo "Не удалось записать $LOGROTATE_FILE"; exit 1; }
sudo chmod 644 "$LOGROTATE_FILE"
if command -v logrotate >/dev/null 2>&1; then
  sudo logrotate -d "$LOGROTATE_FILE" >/dev/null 2>&1 || { echo "Ошибка в правилах logrotate: sudo logrotate -d $LOGROTATE_FILE"; exit 1; }
else
  echo "⚠️  logrotate не установлен: sudo apt install logrotate"
fi

sudo systemctl daemon-reload || { echo "Ошибка при перезагрузке systemd"; exit 1; }
sudo systemctl enable "$SERVICE_NAME" || { echo "Не удалось включить службу $SERVICE_NAME"; exit 1; }

echo "Запуск службы $SERVICE_NAME..."
sudo systemctl start "$SERVICE_NAME" || { echo "Не удалось запустить службу $SERVICE_NAME"; exit 1; }

echo "cbrservice успешно обновлён и перезапущен. Логи: $CBR_HOME/logs/cbrservice.log"
