// Command cbrconvert — отдельная служба, которая переносит завершённые записи из
// ~/.cbr/recordings в медиатеку ~/.cbr/media в виде HLS без перекодирования
// (ffmpeg -c copy) и удаляет исходники. См. internal/converter.
//
// Использование:
//
//	cbrconvert [-interval 5m] [-min-age 1m] [-ffmpeg ffmpeg] [-debug]  работать как служба
//	cbrconvert -once                                                   один проход и выход
//	cbrconvert -version                                                показать версию
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"cbrservice/internal/converter"
	"cbrservice/internal/home"
)

// version задаётся при сборке: -ldflags "-X main.version=..."
var version = "dev"

// lockFile — в data/: не даёт запустить два экземпляра над одной служебной папкой.
const lockFile = "cbrconvert.lock"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cbrconvert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	interval := fs.Duration("interval", converter.DefaultInterval, "pause between scans of recordings/")
	minAge := fs.Duration("min-age", converter.DefaultMinAge, "skip recordings modified more recently than this")
	ffmpeg := fs.String("ffmpeg", "ffmpeg", "path to ffmpeg")
	once := fs.Bool("once", false, "convert pending recordings once and exit")
	debug := fs.Bool("debug", false, "enable debug logging")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, version)
		return 0
	}
	if *interval <= 0 {
		fmt.Fprintln(stderr, "-interval must be positive")
		return 2
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	ffmpegPath, err := exec.LookPath(*ffmpeg)
	if err != nil {
		log.Error("ffmpeg not found", "ffmpeg", *ffmpeg, "err", err)
		return 1
	}

	layout, err := home.Resolve()
	if err == nil {
		err = layout.Ensure()
	}
	if err != nil {
		log.Error("prepare home", "err", err)
		return 1
	}

	unlock, err := lock(filepath.Join(layout.Data, lockFile))
	if err != nil {
		log.Error("lock", "err", err)
		return 1
	}
	defer unlock()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	conv := &converter.Converter{
		Recordings: layout.Recordings,
		Media:      layout.Media,
		FFmpeg:     ffmpegPath,
		MinAge:     *minAge,
		Log:        log,
	}

	if *once {
		n := conv.RunOnce(ctx)
		log.Info("done", "published", n)
		return 0
	}

	log.Info("starting cbrconvert", "version", version, "home", layout.Root,
		"interval", *interval, "min_age", *minAge, "ffmpeg", ffmpegPath)
	conv.Run(ctx, *interval)
	log.Info("cbrconvert stopped")
	return 0
}

// lock берёт эксклюзивную блокировку файла (flock); она снимается и при падении процесса.
func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another cbrconvert is already running (%s)", path)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
