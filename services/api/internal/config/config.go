// Package config — настройки api: ~/.cbr/config/api.json (необязательный файл).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	coreconfig "cbr/core/config"
	"cbr/core/configcli"
)

// Name — имя службы и файла конфига.
const Name = "api"

// SectionStatusAPI — имя секции подключения в api.json.
const SectionStatusAPI = "status_api"

// DefaultAddr — адрес HTTP API по умолчанию.
const DefaultAddr = ":8000"

// Config — содержимое api.json.
type Config struct {
	// Addr — адрес для прослушивания, например ":8000".
	Addr string `json:"addr"`
	// StatusBaseURL — базовый адрес status API, как base_url в recorder.json; нужен для
	// GET /watchlist/:user/live (необязательно: без него живой просмотр выключен).
	StatusBaseURL string `json:"status_base_url"`
	// StatusAPI — подключение к status API.
	StatusAPI coreconfig.Connection `json:"status_api"`
	// LegacyProxy — ключ "proxy" прежней нотации; непустой — ошибка (coreconfig.LegacyProxyError).
	LegacyProxy string `json:"proxy,omitempty"`
}

// Load читает и проверяет конфиг. Если файла нет, используются значения по умолчанию.
func Load(path string) (*Config, error) {
	var cfg Config
	err := coreconfig.Load(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		cfg.Normalize()
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// CLI — подкоманда "api config", которая пишет конфиг в path.
func CLI(path string) *configcli.Command {
	cfg := &Config{}
	return &configcli.Command{
		Service: Name,
		Path:    path,
		Config:  cfg,
		Params: []configcli.Param{
			configcli.String("addr", "addr", "<host:port>", "listen address, "+DefaultAddr+" when not set", false, &cfg.Addr),
			configcli.URL("status-base-url", "status_base_url", "status API base URL, same as base_url of recorder; live streams are off without it", false, &cfg.StatusBaseURL),
			configcli.Connection("status-api", SectionStatusAPI, "connection to the status API", &cfg.StatusAPI),
		},
		Migrate: func() []string {
			if note := coreconfig.MigrateLegacyProxy(&cfg.LegacyProxy, &cfg.StatusAPI, SectionStatusAPI); note != "" {
				return []string{note}
			}
			return nil
		},
		Examples: []string{
			"config",
			"config --addr :8000 --status-base-url https://example.com/api/streamcontext",
			"config --status-api http://127.0.0.1:8080",
			`config --status-base-url ""`,
		},
	}
}

// Normalize подставляет значения по умолчанию.
func (c *Config) Normalize() {
	c.Addr = strings.TrimSpace(c.Addr)
	if c.Addr == "" {
		c.Addr = DefaultAddr
	}
	c.StatusBaseURL = strings.TrimRight(strings.TrimSpace(c.StatusBaseURL), "/")
	c.StatusAPI.Normalize()
}

// Validate проверяет корректность URL.
func (c *Config) Validate() error {
	var errs []error
	if c.StatusBaseURL != "" {
		if err := coreconfig.ValidateURL(c.StatusBaseURL); err != nil {
			errs = append(errs, fmt.Errorf("status_base_url: %w", err))
		}
	}
	errs = append(errs,
		c.StatusAPI.Validate(SectionStatusAPI),
		coreconfig.LegacyProxyError(c.LegacyProxy, SectionStatusAPI),
	)
	return errors.Join(errs...)
}
