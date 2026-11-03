#!/usr/bin/env bash
shopt -s nullglob

for ts in *.ts; do
  mp4="${ts%.ts}.mp4"
  echo "Remux $ts → $mp4"
  ffmpeg -fflags +genpts -i "$ts" -c copy -movflags +faststart "$mp4"
done