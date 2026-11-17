// Command recorder — служба записи стримов: следит за пользователями из
// списка слежения (общая база ~/.cbr/data/cbr.db) и записывает их стримы
// в ~/.cbr/recordings.
//
// Использование:
//
//	recorder [-debug]          запустить службу
//	recorder config [флаги]    записать ~/.cbr/config/recorder.json: без флагов спросит все
//	                           параметры, с флагами задаст переданные (recorder config --help)
//	recorder -version          показать версию
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"cbr/core/configcli"
	"cbr/core/home"
	"cbr/core/svc"
	"cbr/recorder/internal/app"
	"cbr/recorder/internal/config"
)

// version задаётся при сборке: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == configcli.Name {
		layout, err := home.Resolve()
		if err != nil {
			fmt.Fprintln(stderr, "❌", err)
			return 1
		}
		return config.CLI(layout.ConfigFile(config.Name)).Run(args[1:], stdin, stdout, stderr)
	}

	fs := flag.NewFlagSet("recorder", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: recorder [-debug] | recorder config [flags] (see: recorder config --help) | recorder -version")
		fs.PrintDefaults()
	}
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
