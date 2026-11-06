#!/usr/bin/env bash
# Диагностика status API и CDN по одному пользователю через recorder/cmd/hlsprobe
# с настройками dev (base_url и proxy из $CBR_HOME/config/recorder.json).
#
#   services/dev_probe.sh <user или ссылка>     пользователь, который сейчас в эфире (public)
#   CBR_HOME=~/.cbr services/dev_probe.sh <user> другая служебная папка (по умолчанию ~/cbr-dev)
#
# Параметры подобраны для разбора поведения recorder, указывать их не нужно:
#   - статус пользователя в нижнем, исходном и верхнем регистре и несуществующего
#     neverexistinguserbla — реальные коды ответов и чувствительность к регистру;
#   - ждёт начала стрима до 5 мин, если он не идёт;
#   - 5 минут перезагрузок media-плейлиста (раз в target duration) — меняются ли
#     токены в URL сегментов, как растёт MEDIA-SEQUENCE, не истекает ли токен;
#   - 4 последних сегмента, ключи, init, свежий hls_source.
#
# Собирает hlsprobe в $CBR_HOME/bin и кладёт результат в корень репозитория:
# hlsprobe-<время>/ (ссылки и имя пользователя замаскированы; папка в .gitignore —
# в ней сегменты и ключи). Ничего не пишет
# в базу и в recordings.
#
# Совместим с bash 3.2 из macOS.

set -u

SERVICES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SERVICES_DIR/.." && pwd)"
export CBR_HOME="${CBR_HOME:-$HOME/cbr-dev}"
NONEXISTENT_USER="neverexistinguserbla"
WAIT="5m"
DURATION="5m"
SEGMENTS=4

die() { echo "❌ $*" >&2; exit 1; }

[ $# -eq 1 ] || die "использование: $0 <user или ссылка> (пользователь в эфире)"
command -v go >/dev/null 2>&1 || die "Go не установлен: brew install go (нужен Go 1.26+)"
[ -f "$CBR_HOME/config/recorder.json" ] || die "нет $CBR_HOME/config/recorder.json — настройте dev (services/dev.sh recorder)"

mkdir -p "$CBR_HOME/bin" || die "не удалось создать $CBR_HOME/bin"
echo "🔨 сборка hlsprobe…"
(cd "$SERVICES_DIR/recorder" && go build -trimpath -o "$CBR_HOME/bin/hlsprobe" ./cmd/hlsprobe) || die "сборка не удалась"

# Результат — в корень репозитория (hlsprobe сам выбирает имя папки).
cd "$REPO_DIR" || die "нет $REPO_DIR"
echo "🔎 CBR_HOME=$CBR_HOME"
"$CBR_HOME/bin/hlsprobe" -nonexistent "$NONEXISTENT_USER" -wait "$WAIT" \
  -duration "$DURATION" -segments "$SEGMENTS" "$1" || die "hlsprobe завершился с ошибкой"
echo "✅ готово: пришлите имя папки hlsprobe-… из $REPO_DIR"
