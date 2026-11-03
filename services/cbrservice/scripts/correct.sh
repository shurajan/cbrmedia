#!/usr/bin/env bash
# fix-ts-current.sh — пакетная обработка всех .ts файлов ТОЛЬКО в текущем каталоге

# Настройки FFmpeg
FFMPEG_FLAGS="-fflags +genpts -err_detect ignore_err"
CODEC_COPY="-c copy"

# Включаем nullglob, чтобы при отсутствии .ts цикл не ушёл на “*.ts”
shopt -s nullglob

# Обрабатываем каждый .ts в текущей папке
for tsfile in *.ts; do
  base=$(basename "$tsfile")
  output="fixed_${base}"

  echo "Обрабатываю: $tsfile → $output"
  ffmpeg $FFMPEG_FLAGS -i "$tsfile" $CODEC_COPY "$output"
done

echo "Готово!"