package hls

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// partialSuffix — суффикс файла, который ещё записывается.
const partialSuffix = ".download"

// segmentWriter склеивает расшифрованные сегменты в part-файлы и переходит на
// новый part после rotateAfter секунд записанного контента. Для fMP4 в начало
// каждого part пишется init-сегмент, чтобы каждый part проигрывался отдельно.
type segmentWriter struct {
	dir         string
	baseName    string
	ext         string
	rotateAfter float64
	log         *slog.Logger

	init    []byte
	initURI string // URI текущего init-сегмента
	// rotatePending — init сменился, следующий сегмент начнёт новый part,
	// потому что фрагменты с другим init нельзя дописывать в тот же файл.
	rotatePending bool
	part          int
	f             *os.File
	tmpPath       string // путь к записываемому .download-файлу
	finPath       string // путь, в который он переименовывается по завершении part
	elapsed       float64
	segments      int // сколько сегментов записано за всю запись
}

// setInit задаёт init-сегмент fMP4. Если содержимое init изменилось, а part
// уже открыт, следующий сегмент будет записан в новый part с новым init.
// Тот же init под другим URI (например, со свежим CDN-токеном) ротации не вызывает.
func (w *segmentWriter) setInit(uri string, b []byte) {
	w.initURI = uri
	if bytes.Equal(b, w.init) {
		return
	}
	w.init = b
	if w.f != nil {
		w.rotatePending = true
		w.log.Info("init segment changed, next segment starts a new part", "uri", uri)
	}
}

func (w *segmentWriter) write(data []byte, dur float64) error {
	if w.f == nil || w.rotatePending || w.elapsed >= w.rotateAfter {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	if _, err := w.f.Write(data); err != nil {
		return err
	}
	w.elapsed += dur
	w.segments++
	return nil
}

func (w *segmentWriter) rotate() error {
	w.close()
	f, err := w.createNextPart()
	if err != nil {
		return err
	}
	w.f = f
	w.elapsed = 0
	w.rotatePending = false
	if len(w.init) > 0 {
		if _, err := w.f.Write(w.init); err != nil {
			return err
		}
	}
	w.log.Info("recording part", "path", w.tmpPath)
	return nil
}

// createNextPart создаёт .download-файл для следующего свободного номера part.
// Имя записи содержит время с точностью до минуты, поэтому повторный старт
// записи того же пользователя в ту же минуту даёт то же baseName; занятые
// номера (готовый файл или .download) пропускаются, а не перезаписываются.
func (w *segmentWriter) createNextPart() (*os.File, error) {
	for {
		w.part++
		w.finPath = filepath.Join(w.dir, fmt.Sprintf("%s_part%d.%s", w.baseName, w.part, w.ext))
		// Настоящее расширение остаётся в имени (…part1.ts.download), чтобы при
		// старте прерванную запись можно было восстановить с правильным типом.
		w.tmpPath = w.finPath + partialSuffix

		if _, err := os.Lstat(w.finPath); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		f, err := os.OpenFile(w.tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return f, err
	}
}

// close завершает текущий part: закрывает .download-файл и переименовывает его
// в настоящее расширение, чтобы готовая (или прерванная) запись всегда имела
// правильный суффикс .ts/.mp4.
func (w *segmentWriter) close() {
	if w.f == nil {
		return
	}
	_ = w.f.Close()
	w.f = nil
	if err := os.Rename(w.tmpPath, w.finPath); err != nil {
		w.log.Error("finalize part", "from", w.tmpPath, "to", w.finPath, "err", err)
	}
}

// RecoverPartial завершает записи, оставшиеся незаконченными после предыдущего
// запуска (жёсткий сбой, отключение питания). Каждый "<name>.<ext>.download"
// содержит валидную частичную запись и переименовывается в "<name>.<ext>".
// Если целевой файл уже существует, остаток удаляется.
func RecoverPartial(dir string, log *slog.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Warn("scan for partial downloads", "dir", dir, "err", err)
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, partialSuffix) {
			continue
		}
		tmp := filepath.Join(dir, name)
		fin := filepath.Join(dir, strings.TrimSuffix(name, partialSuffix))
		if _, err := os.Stat(fin); err == nil {
			if err := os.Remove(tmp); err != nil {
				log.Error("remove stale partial download", "path", tmp, "err", err)
			}
			continue
		}
		if err := os.Rename(tmp, fin); err != nil {
			log.Error("recover partial recording", "from", tmp, "to", fin, "err", err)
		} else {
			log.Info("recovered partial recording", "path", fin)
		}
	}
}
