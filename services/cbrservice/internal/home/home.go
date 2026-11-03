// Package home описывает служебную папку cbrservice (по умолчанию ~/.cbr).
// Все файлы и директории, нужные сервису, лежат внутри неё:
//
//	~/.cbr/
//	├── bin/          бинарник демона
//	├── config/       config.json, auth.json
//	├── data/         базы данных (bbolt)
//	├── recordings/   сырые записи стримов (watcher)
//	├── media/        HLS-библиотека для плеера (mediafs)
//	└── logs/         логи службы
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

// Ensure создаёт все директории служебной папки, если их нет.
func (l Layout) Ensure() error {
	for _, dir := range []string{l.Root, l.Bin, l.Config, l.Data, l.Recordings, l.Media, l.Logs} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("can't create %s: %w", dir, err)
		}
	}
	return nil
}

// ConfigFile — путь к config.json (настройки watcher).
func (l Layout) ConfigFile() string { return filepath.Join(l.Config, "config.json") }

// AuthFile — путь к auth.json (хэш пароля mediafs).
func (l Layout) AuthFile() string { return filepath.Join(l.Config, "auth.json") }
