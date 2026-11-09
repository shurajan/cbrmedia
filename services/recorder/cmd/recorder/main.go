// Command recorder — служба записи стримов: следит за пользователями из
// списка слежения (общая база ~/.cbr/data/cbr.db) и записывает их стримы
// в ~/.cbr/recordings.
//
// Использование:
//
//	recorder [-debug]   запустить службу
//	recorder -version   показать версию
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"cbr/core/home"
	"cbr/core/svc"
	"cbr/recorder/internal/app"
)

// version задаётся при сборке: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("recorder", flag.ContinueOnError)
	fs.SetOutput(stderr)
	debug := fs.Bool("debug", false, "enable debug logging")
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

	log.Info("starting recorder", "version", version)
	if err := app.Run(ctx, layout, app.Options{Version: version}, log); err != nil {
		log.Error("recorder stopped with error", "err", err)
		return 1
	}
	return 0
}
