// Command converter — отдельная служба, которая переносит завершённые записи из
// ~/.cbr/recordings в медиатеку ~/.cbr/media в виде HLS без перекодирования
// (ffmpeg -c copy) и удаляет исходники. См. internal/converter.
//
// Использование:
//
//	converter [-interval 5m] [-min-age 1m] [-ffmpeg ffmpeg] [-ffprobe ffprobe] [-debug]  работать как служба
//	converter -once                                                   один проход и выход
//	converter -version                                                показать версию
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
	"sync"
	"syscall"

	"cbr/converter/internal/converter"
	"cbr/core/home"
	"cbr/core/store"
)

// version задаётся при сборке: -ldflags "-X main.version=..."
var version = "dev"

// lockFile — в data/: не даёт запустить два экземпляра над одной служебной папкой.
const lockFile = "converter.lock"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("converter", flag.ContinueOnError)
	fs.SetOutput(stderr)
	interval := fs.Duration("interval", converter.DefaultInterval, "pause between scans of recordings/")
	minAge := fs.Duration("min-age", converter.DefaultMinAge, "skip recordings modified more recently than this")
	ffmpeg := fs.String("ffmpeg", "ffmpeg", "path to ffmpeg")
	ffprobe := fs.String("ffprobe", "", "path to ffprobe for video resolution in meta.json (default: next to ffmpeg or from PATH)")
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
	ffprobePath := findFFprobe(*ffprobe, ffmpegPath)
	if ffprobePath == "" {
		log.Warn("ffprobe not found: videos will be published without resolution")
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
		FFprobe:    ffprobePath,
		MinAge:     *minAge,
		Log:        log,
	}

	if *once {
		n := conv.RunOnce(ctx)
		log.Info("done", "published", n)
		return 0
	}

	// База нужна только для heartbeat (GET /health у api); записи converter
	// по-прежнему ищет в файлах.
	db, err := store.Open(layout.DBFile())
	if err != nil {
		log.Error("open db", "err", err)
		return 1
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("close db", "err", err)
		}
	}()
	var wg sync.WaitGroup
	wg.Go(func() { db.RunHeartbeat(ctx, store.ServiceConverter, version, store.DefaultHeartbeatInterval, log) })

	log.Info("starting converter", "version", version, "home", layout.Root,
		"interval", *interval, "min_age", *minAge, "ffmpeg", ffmpegPath, "ffprobe", ffprobePath)
	conv.Run(ctx, *interval)
	wg.Wait()
	log.Info("converter stopped")
	return 0
}

// findFFprobe возвращает путь к ffprobe: заданный флагом, рядом с ffmpeg или из PATH; "" — не найден.
func findFFprobe(flagValue, ffmpegPath string) string {
	if flagValue != "" {
		if p, err := exec.LookPath(flagValue); err == nil {
			return p
		}
		return ""
	}
	if p, err := exec.LookPath(filepath.Join(filepath.Dir(ffmpegPath), "ffprobe")); err == nil {
		return p
	}
	if p, err := exec.LookPath("ffprobe"); err == nil {
		return p
	}
	return ""
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
			return nil, fmt.Errorf("another converter is already running (%s)", path)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
