// Command cbrservice — единый сервис: записывает стримы и раздаёт их плееру.
//
// Использование:
//
//	cbrservice [-log] [-debug]                 запустить сервис
//	cbrservice hash-password --password <pwd>  задать пароль плеера (~/.cbr/config/auth.json)
//	cbrservice -version                        показать версию
//
// Все данные сервиса лежат в служебной папке ~/.cbr (или $CBR_HOME), см. internal/home.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"cbrservice/internal/app"
	"cbrservice/internal/auth"
	"cbrservice/internal/home"
)

// version задаётся при сборке: -ldflags "-X main.version=..."
var version = "dev"

const cmdHashPassword = "hash-password"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == cmdHashPassword {
		return runHashPassword(args[1:], stdin, stdout, stderr)
	}

	fs := flag.NewFlagSet("cbrservice", flag.ContinueOnError)
	fs.SetOutput(stderr)
	logRequests := fs.Bool("log", false, "log every HTTP request")
	debug := fs.Bool("debug", false, "enable debug logging")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, version)
		return 0
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	layout, err := prepareHome()
	if err != nil {
		log.Error("prepare home", "err", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("starting cbrservice", "version", version, "home", layout.Root)
	if err := app.Run(ctx, layout, app.Options{LogRequests: *logRequests}, log); err != nil {
		log.Error("cbrservice stopped with error", "err", err)
		return 1
	}
	log.Info("cbrservice stopped")
	return 0
}

// prepareHome определяет служебную папку, создаёт её структуру и делает её
// рабочей папкой процесса, независимо от того, откуда запущен бинарник.
func prepareHome() (home.Layout, error) {
	layout, err := home.Resolve()
	if err != nil {
		return layout, err
	}
	if err := layout.Ensure(); err != nil {
		return layout, err
	}
	if err := os.Chdir(layout.Root); err != nil {
		return layout, fmt.Errorf("chdir to %s: %w", layout.Root, err)
	}
	return layout, nil
}

// runHashPassword сохраняет хэш пароля плеера в auth.json.
// Если файл уже есть, перезапись требует подтверждения "yes".
func runHashPassword(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(cmdHashPassword, flag.ContinueOnError)
	fs.SetOutput(stderr)
	password := fs.String("password", "", "password to hash and save")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *password == "" {
		fmt.Fprintln(stderr, "❌ Please provide --password")
		return 2
	}

	layout, err := prepareHome()
	if err != nil {
		fmt.Fprintln(stderr, "❌", err)
		return 1
	}
	path := layout.AuthFile()

	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(stdout, "⚠️  %s already exists. Overwrite? Type 'yes' to confirm: ", path)
		answer, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintln(stderr, "❌", err)
			return 1
		}
		if strings.TrimSpace(answer) != "yes" {
			fmt.Fprintln(stdout, "❌ Aborted.")
			return 1
		}
	}

	if err := auth.NewService(path).SetPassword(*password); err != nil {
		fmt.Fprintln(stderr, "❌ Failed to save password:", err)
		return 1
	}
	fmt.Fprintln(stdout, "✅ Password hash saved to:", path)
	return 0
}
