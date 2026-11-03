#!/bin/bash

set -euo pipefail

if [ $# -lt 1 ]; then
  echo "❌ Usage: $0 input.ts [options]"
  echo "Options:"
  echo "  --hls             Generate HLS playlist + segments"
  echo "  --sprite          Generate preview frames and VTT"
  echo "  --keep            Keep original .ts file after processing"
  echo "  --slow            Run ffmpeg in low-CPU mode (nice + low threads)"
  echo "  --all             Do everything (default)"
  exit 1
fi

INPUT="$1"
shift

# Flags
DO_HLS=false
DO_SPRITE=false
KEEP_INPUT=false
LOW_CPU_MODE=false

if [ $# -eq 0 ]; then
  DO_HLS=true
  DO_SPRITE=true
else
  for arg in "$@"; do
    case $arg in
      --hls) DO_HLS=true ;;
      --sprite) DO_SPRITE=true ;;
      --keep) KEEP_INPUT=true ;;
      --slow) LOW_CPU_MODE=true ;;
      --all) DO_HLS=true; DO_SPRITE=true ;;
      *) echo "❌ Unknown option: $arg"; exit 1 ;;
    esac
  done
fi

# Validate input
if [[ "$INPUT" != *.ts ]]; then
  echo "❌ Only .ts files are supported"
  exit 1
fi

BASENAME=$(basename "$INPUT" .ts)
OUTPUT_DIR="./$BASENAME"
LOCK_FILE="/tmp/build_media.lock"
LOG_FILE="$OUTPUT_DIR/build.log"
FRAMES_DIR="$OUTPUT_DIR/sprites"
VTT_FILE="$FRAMES_DIR/thumbnails.vtt"

THUMB_WIDTH=480
THUMB_HEIGHT=270

# Блокировка выполнения
if [ -e "$LOCK_FILE" ]; then
  echo "❌ Another instance is already running ($LOCK_FILE)"
  exit 1
fi
trap 'rm -f "$LOCK_FILE"' EXIT
touch "$LOCK_FILE"

# Проверка выходной папки
if [ -d "$OUTPUT_DIR" ]; then
  echo "❌ Output directory $OUTPUT_DIR already exists."
  exit 1
fi

mkdir -p "$OUTPUT_DIR"
mkdir -p "$FRAMES_DIR"

# FFmpeg настройки
if $LOW_CPU_MODE; then
  echo "⚡ Using low-CPU mode..."
  FFMPEG_PREFIX="nice -n 10 ffmpeg -threads 2 -nostdin"
else
  FFMPEG_PREFIX="ffmpeg -nostdin"
fi

# Логирование
current_start_time=0

log_start() {
  echo "▶️  Starting $1..." | tee -a "$LOG_FILE"
  date "+%Y-%m-%d %H:%M:%S" | tee -a "$LOG_FILE"
  current_start_time=$(date +%s)
}

log_done() {
  local now=$(date +%s)
  local elapsed=$((now - current_start_time))
  echo "✅ Finished $1 (⏱ ${elapsed}s)" | tee -a "$LOG_FILE"
  echo "" >> "$LOG_FILE"
}

log_fail() {
  local now=$(date +%s)
  local elapsed=$((now - current_start_time))
  echo "❌ Failed $1 (⏱ ${elapsed}s)" | tee -a "$LOG_FILE"
  echo "" >> "$LOG_FILE"
}

# Start processing
if $DO_HLS; then
  log_start "HLS generation"
  mkdir -p "$OUTPUT_DIR/segments"

  if eval $FFMPEG_PREFIX -i "$INPUT" \
    -c:v copy \
    -c:a copy \
    -hls_time 5 \
    -hls_segment_type mpegts \
    -hls_flags independent_segments \
    -hls_segment_filename "$OUTPUT_DIR/segments/%d.ts" \
    -hls_base_url "segments/" \
    -hls_list_size 0 \
    -f hls "$OUTPUT_DIR/playlist.m3u8"; then
    log_done "HLS generation"
  else
    log_fail "HLS generation"
  fi
fi

if $DO_SPRITE; then
  log_start "Frames and VTT generation"

  # Узнаём длительность видео
  DURATION=$(ffprobe -v error -select_streams v:0 -show_entries format=duration -of csv=p=0 "$INPUT" | cut -d'.' -f1)
  if [[ -z "$DURATION" ]]; then
    echo "❌ Failed to detect duration"
    exit 1
  fi

  echo "ℹ️  Video duration: $DURATION seconds" | tee -a "$LOG_FILE"

  # Генерация кадров
  if eval $FFMPEG_PREFIX -i "$INPUT" \
    -vf fps=1/5,scale=${THUMB_WIDTH}:${THUMB_HEIGHT} \
    -q:v 2 \
    "$FRAMES_DIR/frame_%05d.jpg"; then
    echo "✅ Frames extracted" | tee -a "$LOG_FILE"
  else
    log_fail "Frame extraction"
  fi

  # Генерация VTT
# Генерация кадров
  if eval $FFMPEG_PREFIX -i "$INPUT" \
    -vf fps=1/5,scale=${THUMB_WIDTH}:${THUMB_HEIGHT} \
    -q:v 2 \
    "$FRAMES_DIR/frame_%05d.jpg"; then
    echo "✅ Frames extracted" | tee -a "$LOG_FILE"
  else
    log_fail "Frame extraction"
  fi

  # Генерация VTT
  echo "WEBVTT" > "$VTT_FILE"
  echo "" >> "$VTT_FILE"

  first=true

  for frame_path in "$FRAMES_DIR"/frame_*.jpg; do
    frame_file=$(basename "$frame_path")
    frame_number=$(echo "$frame_file" | sed -E 's/frame_0*([0-9]+)\.jpg/\1/')

    if $first; then
      START=0
      END=5
      first=false
    else
      START=$(( (frame_number - 1) * 5 ))
      END=$(( START + 5 ))
    fi

    start_time=$(printf "%02d:%02d:%02d.000" $((START/3600)) $(( (START%3600)/60 )) $((START%60)))
    end_time=$(printf "%02d:%02d:%02d.000" $((END/3600)) $(( (END%3600)/60 )) $((END%60)))

    echo "$start_time --> $end_time" >> "$VTT_FILE"
    echo "$frame_file" >> "$VTT_FILE"
    echo "" >> "$VTT_FILE"
  done

  echo "✅ VTT written to $VTT_FILE" | tee -a "$LOG_FILE"
  log_done "Frames and VTT generation"
fi

# Удалить оригинальный .ts файл если не указан --keep
if ! $KEEP_INPUT; then
  echo "🗑️  Deleting original file: $INPUT" | tee -a "$LOG_FILE"
  rm -f "$INPUT"
else
  echo "📁 Keeping original file: $INPUT" | tee -a "$LOG_FILE"
fi