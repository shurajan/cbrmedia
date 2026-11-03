// Package config загружает и проверяет config.json сервиса.
//
// Формат файла совместим с прежним Watcher: неизвестные поля (например,
// удалённый url_poller.cert_path) игнорируются.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Значения по умолчанию.
const (
	DefaultHTTPAddr = ":8000"
	DefaultDBFile   = "data.db"
)

// Config — содержимое config.json.
type Config struct {
	// BaseURL — базовый адрес status API; пользователь опрашивается по <BaseURL>/<user>/.
	BaseURL  string   `json:"base_url"`
	Storage  Storage  `json:"storage"`
	Poller   Poller   `json:"url_poller"`
	Telegram Telegram `json:"telegram"`
	HTTP     HTTP     `json:"http"`
}

// Storage — настройки хранилища списка пользователей.
type Storage struct {
	// DB — путь к файлу bbolt. Относительный путь разрешается от data-директории.
	DB string `json:"db"`
}

// Poller — настройки опроса status API и скачивания стримов.
type Poller struct {
	Proxy string `json:"proxy"`
}

// Telegram — настройки Telegram-бота.
type Telegram struct {
	BotToken      string `json:"bot_token"`
	AllowedUserID int64  `json:"allowed_user_id"`
	Proxy         string `json:"proxy"`
}

// HTTP — настройки HTTP-сервера для плеера.
type HTTP struct {
	Addr string `json:"addr"`
}

// Load читает файл, применяет значения по умолчанию, делает относительный
// путь к БД абсолютным относительно dataDir и проверяет результат.
func Load(path, dataDir string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	cfg.applyDefaults(dataDir)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &cfg, nil
}

func (c *Config) applyDefaults(dataDir string) {
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.Poller.Proxy = strings.TrimSpace(c.Poller.Proxy)
	c.Telegram.Proxy = strings.TrimSpace(c.Telegram.Proxy)

	if c.Storage.DB == "" {
		c.Storage.DB = DefaultDBFile
	}
	if !filepath.IsAbs(c.Storage.DB) {
		c.Storage.DB = filepath.Join(dataDir, c.Storage.DB)
	}
	if c.HTTP.Addr == "" {
		c.HTTP.Addr = DefaultHTTPAddr
	}
}

// Validate проверяет обязательные поля и корректность URL.
func (c *Config) Validate() error {
	var errs []error

	if c.BaseURL == "" {
		errs = append(errs, errors.New("base_url is required"))
	} else if err := validateURL(c.BaseURL); err != nil {
		errs = append(errs, fmt.Errorf("base_url: %w", err))
	}
	if c.Telegram.BotToken == "" {
		errs = append(errs, errors.New("telegram.bot_token is required"))
	}
	if c.Telegram.AllowedUserID == 0 {
		errs = append(errs, errors.New("telegram.allowed_user_id is required"))
	}
	if c.Poller.Proxy != "" {
		if err := validateURL(c.Poller.Proxy); err != nil {
			errs = append(errs, fmt.Errorf("url_poller.proxy: %w", err))
		}
	}
	if c.Telegram.Proxy != "" {
		if err := validateURL(c.Telegram.Proxy); err != nil {
			errs = append(errs, fmt.Errorf("telegram.proxy: %w", err))
		}
	}
	return errors.Join(errs...)
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%q must be an absolute URL", raw)
	}
	return nil
}
