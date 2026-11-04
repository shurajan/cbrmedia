// Package home описывает служебную папку cbrmedia (по умолчанию ~/.cbr), общую
// для всех служб. Все файлы и директории служб лежат внутри неё:
//
//	~/.cbr/
//	├── bin/          бинарники recorder, tbot, api, converter
//	├── config/       <служба>.json, auth.json
//	├── data/         cbr.db (SQLite), блокировки
//	├── recordings/   сырые записи стримов (recorder)
//	├── media/        HLS-библиотека для плеера (converter → api)
//	└── logs/         логи служб
//
// Путь можно переопределить переменной окружения CBR_HOME.
package home

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnvHome — переменная окружения для переопределения корня служебной папки.
const EnvHome = "CBR_HOME"

// DBFileName — имя общей базы в data/.
const DBFileName = "cbr.db"

// Layout — абсолютные пути служебной папки.
type Layout struct {
	Root       string
	Bin        string
	Config     string
	Data       string
	Recordings string
	Media      string
	Logs       string
}

// Resolve определяет корень ($CBR_HOME или ~/.cbr) и строит пути. Директории не создаёт.
func Resolve() (Layout, error) {
	root := os.Getenv(EnvHome)
	if root == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Layout{}, fmt.Errorf("can't find home dir: %w", err)
		}
		root = filepath.Join(userHome, ".cbr")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Layout{}, fmt.Errorf("can't resolve %s: %w", root, err)
	}
	return Layout{
		Root:       root,
		Bin:        filepath.Join(root, "bin"),
		Config:     filepath.Join(root, "config"),
		Data:       filepath.Join(root, "data"),
		Recordings: filepath.Join(root, "recordings"),
		Media:      filepath.Join(root, "media"),
		Logs:       filepath.Join(root, "logs"),
	}, nil
}

// Prepare определяет служебную папку, создаёт её структуру и делает её рабочей
// папкой процесса, независимо от того, откуда запущен бинарник.
func Prepare() (Layout, error) {
	layout, err := Resolve()
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

// Ensure создаёт все директории служебной папки, если их нет.
func (l Layout) Ensure() error {
	for _, dir := range []string{l.Root, l.Bin, l.Config, l.Data, l.Recordings, l.Media, l.Logs} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("can't create %s: %w", dir, err)
		}
	}
	return nil
}

// ConfigFile — путь к конфигу службы: config/<service>.json.
func (l Layout) ConfigFile(service string) string {
	return filepath.Join(l.Config, service+".json")
}

// AuthFile — путь к auth.json (хэш пароля и токен плеера, служба api).
func (l Layout) AuthFile() string { return filepath.Join(l.Config, "auth.json") }

// DBFile — путь к общей базе data/cbr.db.
func (l Layout) DBFile() string { return filepath.Join(l.Data, DBFileName) }
