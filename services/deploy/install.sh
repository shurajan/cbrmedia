#!/bin/bash
# Сборка и (пере)установка служб cbrmedia как systemd-служб на Ubuntu.
#
#   services/deploy/install.sh                      все службы: recorder tbot api converter
#   services/deploy/install.sh recorder api         только перечисленные
#   NO_PULL=1 services/deploy/install.sh tbot       без git pull
#   CONVERT_INTERVAL=10m CONVERT_MIN_AGE=1m services/deploy/install.sh converter
#
# Каждая служба — отдельный unit cbr-<служба>.service, их можно включать и
# выключать независимо: sudo systemctl disable --now cbr-tbot.
# Всё, что нужно службам, лежит в служебной папке ~/.cbr (см. services/README.md).
# Скрипт также ставит ротацию логов в /etc/logrotate.d/cbrmedia и останавливает
# прежние монолитные службы cbrservice/cbrconvert, если они остались.

set -u

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICES_DIR="$(cd "$DEPLOY_DIR/.." && pwd)"
CBR_HOME="${CBR_HOME:-$HOME/.cbr}"
# Пользователь служб. Не $USER: он не задан при запуске из cron/CI/docker exec,
# и пустой User= молча запустил бы службу от root.
RUN_USER="$(id -un)"
RUN_GROUP="$(id -gn)"
LOGROTATE_FILE="/etc/logrotate.d/cbrmedia"
CONVERT_INTERVAL="${CONVERT_INTERVAL:-5m}"
CONVERT_MIN_AGE="${CONVERT_MIN_AGE:-1m}"
ALL_SERVICES=(recorder tbot api converter)

die() { echo "❌ $*" >&2; exit 1; }

if [ $# -eq 0 ]; then
  SERVICES=("${ALL_SERVICES[@]}")
else
  SERVICES=("$@")
  for svc in "${SERVICES[@]}"; do
    [[ " ${ALL_SERVICES[*]} " == *" $svc "* ]] || die "неизвестная служба: $svc (есть: ${ALL_SERVICES[*]})"
  done
fi

command -v go >/dev/null 2>&1 || die "Go не установлен (нужен Go 1.26+)"

if [ -z "${NO_PULL:-}" ]; then
  echo "Обновление репозитория..."
  git -C "$SERVICES_DIR" pull || die "не удалось выполнить git pull"
fi
VERSION="$(git -C "$SERVICES_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)"

echo "Подготовка служебной папки $CBR_HOME..."
mkdir -p "$CBR_HOME"/{bin,config,data,recordings,media,logs} || die "не удалось создать $CBR_HOME"
# Служебная папка — только для пользователя служб и его группы (mkdir -p создаёт
# с правами по umask, обычно 0755); chmod исправляет и уже существующие папки.
chmod 750 "$CBR_HOME" "$CBR_HOME"/{bin,config,data,recordings,media,logs} || die "не удалось задать права на $CBR_HOME"

# --- прежние службы ------------------------------------------------------------
for legacy in cbrservice cbrconvert; do
  if [ -f "/etc/systemd/system/$legacy.service" ]; then
    echo "Отключение прежней службы $legacy.service..."
    sudo systemctl disable --now "$legacy.service" || die "не удалось остановить $legacy.service"
    sudo rm -f "/etc/systemd/system/$legacy.service"
  fi
done
sudo rm -f /etc/logrotate.d/cbrservice # заменён на $LOGROTATE_FILE

# --- ротация логов -------------------------------------------------------------
echo "Запись правил ротации логов $LOGROTATE_FILE..."
sed -e "s|@CBR_HOME@|$CBR_HOME|g" -e "s|@USER@|$RUN_USER|g" -e "s|@GROUP@|$RUN_GROUP|g" \
  "$DEPLOY_DIR/logrotate.conf" | sudo tee "$LOGROTATE_FILE" >/dev/null || die "не удалось записать $LOGROTATE_FILE"
sudo chmod 644 "$LOGROTATE_FILE"
if command -v logrotate >/dev/null 2>&1; then
  sudo logrotate -d "$LOGROTATE_FILE" >/dev/null 2>&1 || die "ошибка в правилах logrotate: sudo logrotate -d $LOGROTATE_FILE"
else
  echo "⚠️  logrotate не установлен: sudo apt install logrotate"
fi

# --- проверки перед установкой ---------------------------------------------------

# ensure_config <служба>: без конфига служба не стартует — кладём шаблон и пропускаем её.
ensure_config() {
  local svc="$1" cfg="$CBR_HOME/config/$1.json"
  [ -f "$cfg" ] && return 0
  cp "$SERVICES_DIR/$svc/config.example.json" "$cfg" && chmod 600 "$cfg"
  echo "⚠️  $svc: создан шаблон $cfg — заполните его и запустите скрипт снова."
  return 1
}

# preflight <служба>: 0 — можно ставить; заодно задаёт EXEC_ARGS и UNIT_EXTRA.
preflight() {
  EXEC_ARGS=""
  UNIT_EXTRA=""
  case "$1" in
    recorder | tbot)
      ensure_config "$1"
      ;;
    api)
      if [ ! -f "$CBR_HOME/config/auth.json" ]; then
        echo "⚠️  api: нет $CBR_HOME/config/auth.json. После установки задайте пароль и запустите скрипт снова:"
        echo "    CBR_HOME=$CBR_HOME $CBR_HOME/bin/api hash-password --password <пароль>"
        NEED_AUTH=1
      fi
      ;;
    converter)
      local ffmpeg
      ffmpeg="$(command -v ffmpeg)" || { echo "⚠️  converter: ffmpeg не установлен: sudo apt install ffmpeg"; return 1; }
      EXEC_ARGS="-interval $CONVERT_INTERVAL -min-age $CONVERT_MIN_AGE -ffmpeg $ffmpeg"
      # Переупаковка — в основном диск, она не должна мешать записи и раздаче видео.
      UNIT_EXTRA=$'Nice=10\nIOSchedulingClass=idle'
      ;;
  esac
}

# --- установка -----------------------------------------------------------------

install_service() {
  local svc="$1"
  local bin="$CBR_HOME/bin/$svc" unit="cbr-$svc.service"
  local unit_file="/etc/systemd/system/$unit" build_output
  NEED_AUTH=""

  echo
  echo "=== $svc ==="
  preflight "$svc" || return 1

  echo "Сборка $svc $VERSION..."
  build_output="$(mktemp)"
  go build -C "$SERVICES_DIR/$svc" -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$build_output" "./cmd/$svc" \
    || { rm -f "$build_output"; echo "❌ $svc: ошибка сборки"; return 1; }

  if systemctl is-active --quiet "$unit"; then
    sudo systemctl stop "$unit" || { echo "❌ не удалось остановить $unit"; return 1; }
  fi
  install -m 0755 "$build_output" "$bin" || { echo "❌ не удалось установить $bin"; return 1; }
  rm -f "$build_output"

  # api без пароля не стартует: бинарник поставлен, чтобы им можно было задать пароль.
  [ -n "$NEED_AUTH" ] && return 1

  # Unit-файл перезаписывается при каждом деплое, чтобы изменения в нём применялись.
  sudo tee "$unit_file" >/dev/null <<EOF || { echo "❌ не удалось записать $unit_file"; return 1; }
[Unit]
Description=cbrmedia $svc
After=network-online.target
Wants=network-online.target

[Service]
User=$RUN_USER
Environment=CBR_HOME=$CBR_HOME
WorkingDirectory=$CBR_HOME
ExecStart=$bin $EXEC_ARGS
Restart=always
RestartSec=5
$UNIT_EXTRA
StandardOutput=append:$CBR_HOME/logs/$svc.log
StandardError=append:$CBR_HOME/logs/$svc.log

[Install]
WantedBy=multi-user.target
EOF

  sudo systemctl daemon-reload || { echo "❌ ошибка daemon-reload"; return 1; }
  sudo systemctl enable "$unit" >/dev/null || { echo "❌ не удалось включить $unit"; return 1; }
  sudo systemctl start "$unit" || { echo "❌ не удалось запустить $unit"; return 1; }
  echo "✅ $svc установлен и запущен. Логи: $CBR_HOME/logs/$svc.log"
}

FAILED=()
for svc in "${SERVICES[@]}"; do
  install_service "$svc" || FAILED+=("$svc")
done

echo
if [ ${#FAILED[@]} -gt 0 ]; then
  echo "⚠️  Не установлены: ${FAILED[*]} (см. сообщения выше)"
  exit 1
fi
echo "Готово: ${SERVICES[*]}"
