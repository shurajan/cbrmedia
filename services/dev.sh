#!/usr/bin/env bash
# Локальная сборка и запуск back-end (macOS или Linux) — для проверки работы.
#
#   services/dev.sh                    все службы: recorder tbot api converter
#   services/dev.sh recorder api       только перечисленные
#   CBR_HOME=~/.cbr services/dev.sh    другая служебная папка (по умолчанию ~/cbr-dev)
#   DEBUG=1 services/dev.sh            подробные логи у всех служб
#   CONVERT_INTERVAL=30s CONVERT_MIN_AGE=10s services/dev.sh converter
#
# Собирает бинарники в $CBR_HOME/bin, проверяет конфиги и запускает службы
# параллельно в этом терминале: вывод каждой — с префиксом [служба], копия в
# $CBR_HOME/logs/<служба>.log. Ctrl+C останавливает все. Если для службы нет
# конфига, скрипт кладёт шаблон и запускает остальные.
#
# Совместим с bash 3.2 из macOS.

set -u

SERVICES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export CBR_HOME="${CBR_HOME:-$HOME/cbr-dev}"
CONVERT_INTERVAL="${CONVERT_INTERVAL:-1m}"
CONVERT_MIN_AGE="${CONVERT_MIN_AGE:-1m}"
ALL_SERVICES="recorder tbot api converter"

die() { echo "❌ $*" >&2; exit 1; }

if [ $# -eq 0 ]; then
  set -- $ALL_SERVICES
fi
for svc in "$@"; do
  case " $ALL_SERVICES " in
    *" $svc "*) ;;
    *) die "неизвестная служба: $svc (есть: $ALL_SERVICES)" ;;
  esac
done

command -v go >/dev/null 2>&1 || die "Go не установлен: brew install go (нужен Go 1.26+)"

mkdir -p "$CBR_HOME"/{bin,config,data,recordings,media,logs} || die "не удалось создать $CBR_HOME"
chmod 750 "$CBR_HOME"
echo "Служебная папка: $CBR_HOME"

# --- сборка ----------------------------------------------------------------------
VERSION="$(git -C "$SERVICES_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)"
for svc in "$@"; do
  echo "🔨 Сборка $svc..."
  go build -C "$SERVICES_DIR/$svc" -ldflags "-X main.version=$VERSION" -o "$CBR_HOME/bin/$svc" "./cmd/$svc" \
    || die "ошибка сборки $svc"
done

# --- проверки --------------------------------------------------------------------

# ensure_config <служба>: без конфига служба не стартует — кладём шаблон.
ensure_config() {
  local cfg="$CBR_HOME/config/$1.json"
  [ -f "$cfg" ] && return 0
  cp "$SERVICES_DIR/$1/config.example.json" "$cfg" && chmod 600 "$cfg"
  echo "⚠️  $1: создан шаблон $cfg — заполните его и запустите скрипт снова."
  return 1
}

# ready <служба>: 0 — службу можно запускать.
ready() {
  case "$1" in
    recorder | tbot) ensure_config "$1" ;;
    api)
      [ -f "$CBR_HOME/config/auth.json" ] && return 0
      echo "⚠️  api: нет пароля плеера. Задайте его и запустите скрипт снова:"
      echo "    CBR_HOME=$CBR_HOME $CBR_HOME/bin/api hash-password --password <пароль>"
      return 1
      ;;
    converter)
      command -v ffmpeg >/dev/null 2>&1 && return 0
      echo "⚠️  converter: ffmpeg не установлен: brew install ffmpeg"
      return 1
      ;;
  esac
}

# args <служба> — аргументы запуска.
args() {
  local debug=""
  [ -n "${DEBUG:-}" ] && debug="-debug"
  case "$1" in
    converter) echo "-interval $CONVERT_INTERVAL -min-age $CONVERT_MIN_AGE $debug" ;;
    *) echo "$debug" ;;
  esac
}

RUN=""
for svc in "$@"; do
  ready "$svc" && RUN="$RUN $svc"
done
[ -n "$RUN" ] || die "нечего запускать"

# --- запуск ----------------------------------------------------------------------
PIDS=""

stop_all() {
  trap - INT TERM
  echo
  echo "⏹  Остановка:$RUN"
  # shellcheck disable=SC2086
  [ -n "$PIDS" ] && kill -TERM $PIDS 2>/dev/null
  wait
  echo "Все службы остановлены."
  exit 0
}
trap stop_all INT TERM

cd "$CBR_HOME" || die "нет $CBR_HOME"
for svc in $RUN; do
  log="$CBR_HOME/logs/$svc.log"
  # shellcheck disable=SC2046
  "$CBR_HOME/bin/$svc" $(args "$svc") \
    > >(tee -a "$log" | awk -v p="[$svc] " '{ print p $0; fflush() }') 2>&1 &
  PIDS="$PIDS $!"
done

echo "🚀 Запущены:$RUN  (логи: $CBR_HOME/logs/, Ctrl+C — остановить)"
case " $RUN " in *" api "*) echo "   API: http://localhost:$(sed -n 's/.*"addr"[^:]*:[[:space:]]*"[^:]*:\([0-9]*\)".*/\1/p' "$CBR_HOME/config/api.json" 2>/dev/null | head -1 | grep . || echo 8000)" ;; esac

# Ждём, пока службы работают; упавшая служба не останавливает остальные.
wait
echo "Все службы завершились."
