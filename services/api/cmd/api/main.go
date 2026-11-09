// Command api — HTTP API для плеера CBRPlayer: авторизация, раздача видео из
// ~/.cbr/media и управление списком слежения через общую базу.
//
// Использование:
//
//	api [-debug]                        запустить службу (-debug: ещё и лог каждого HTTP-запроса)
//	api hash-password --password <pwd>  задать пароль плеера (~/.cbr/config/auth.json)
//	api -version                        показать версию
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"cbr/api/internal/app"
	"cbr/api/internal/auth"
	"cbr/core/home"
	"cbr/core/svc"
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

	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	fs.SetOutput(stderr)
	debug := fs.Bool("debug", false, "enable debug logging, including every HTTP request")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, version)
		return 0
	}

	log := svc.Logger(stderr, *debug)
	layout, err := home.Prepare()
	if err != nil {
		log.Error("prepare home", "err", err)
		return 1
	}

	ctx, stop := svc.SignalContext()
	defer stop()

	log.Info("starting api", "version", version, "home", layout.Root)
	if err := app.Run(ctx, layout, version, log); err != nil {
		log.Error("api stopped with error", "err", err)
		return 1
	}
	log.Info("api stopped")
	return 0
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

	layout, err := home.Prepare()
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
