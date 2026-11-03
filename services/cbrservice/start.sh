#!/usr/bin/env bash
# Build and start cbrservice locally (macOS or Linux).
# All service data lives in $CBR_HOME (default ~/.cbr): config/, data/, recordings/, media/, logs/.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CBR_HOME="${CBR_HOME:-$HOME/.cbr}"
export CBR_HOME
BIN="$CBR_HOME/bin/cbrservice"

if ! command -v go >/dev/null 2>&1; then
  echo "❌ Go is not installed. Install it from https://go.dev/dl/" >&2
  exit 1
fi

echo "🔨 Building cbrservice..."
mkdir -p "$CBR_HOME/bin" "$CBR_HOME/config"
go build -C "$SCRIPT_DIR" -o "$BIN" ./cmd/cbrservice

if [ ! -f "$CBR_HOME/config/config.json" ]; then
  cp "$SCRIPT_DIR/deploy/config.example.json" "$CBR_HOME/config/config.json"
  chmod 600 "$CBR_HOME/config/config.json"
  echo "⚠️  Created $CBR_HOME/config/config.json from template — fill it in and run again."
  exit 1
fi

if [ ! -f "$CBR_HOME/config/auth.json" ]; then
  echo "⚠️  No auth.json found. Create one first:"
  echo "    $BIN hash-password --password <your-password>"
  exit 1
fi

echo "🚀 Starting cbrservice (home: $CBR_HOME)..."
exec "$BIN" "$@"
