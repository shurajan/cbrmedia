// Package config — настройки recorder: ~/.cbr/config/recorder.json.
package config

import (
	"errors"
	"fmt"
	"strings"

	coreconfig "cbr/core/config"
	"cbr/core/configcli"
)

// Name — имя службы и файла конфига.
const Name = "recorder"

// Имена секций подключения в recorder.json.
const (
	SectionStatusAPI = "status_api"
	SectionCDN       = "cdn"
)

// Config — содержимое recorder.json.
type Config struct {
	// BaseURL — базовый адрес status API; пользователь опрашивается по <BaseURL>/<user>/.
	BaseURL string `json:"base_url"`
	// StatusAPI — подключение к status API (опрос статуса и ссылки на стрим).
	StatusAPI coreconfig.Connection `json:"status_api"`
	// CDN — подключение к CDN, с которого качаются плейлисты, ключи и сегменты (hls_source).
	CDN coreconfig.Connection `json:"cdn"`
	// LegacyProxy — ключ "proxy" прежней нотации; непустой — ошибка (coreconfig.LegacyProxyError).
	LegacyProxy string `json:"proxy,omitempty"`
}

// Load читает и проверяет конфиг.
func Load(path string) (*Config, error) {
	var cfg Config
	if err := coreconfig.Load(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// CLI — подкоманда "recorder config", которая пишет конфиг в path.
func CLI(path string) *configcli.Command {
	cfg := &Config{}
	return &configcli.Command{
		Service: Name,
		Path:    path,
		Config:  cfg,
		Params: []configcli.Param{
			configcli.URL("base-url", "base_url", "status API base URL; a user is polled at <base_url>/<user>/", true, &cfg.BaseURL),
			configcli.Connection("status-api", SectionStatusAPI, "connection to the status API", &cfg.StatusAPI),
			configcli.Connection("cdn", SectionCDN, "connection to the CDN (playlists, keys, segments)", &cfg.CDN),
		},
		Migrate: func() []string {
			if note := coreconfig.MigrateLegacyProxy(&cfg.LegacyProxy, &cfg.StatusAPI, SectionStatusAPI); note != "" {
				return []string{note}
			}
			return nil
		},
		Examples: []string{
			"config",
			"config --base-url https://example.com/api/streamcontext --status-api http://127.0.0.1:8080 --cdn direct",
			"config --cdn http://127.0.0.1:8081",
			"config --status-api direct",
		},
	}
}

// Normalize убирает пробелы по краям и подставляет значения по умолчанию.
func (c *Config) Normalize() {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.StatusAPI.Normalize()
	c.CDN.Normalize()
}

// Validate проверяет обязательные поля и корректность URL.
func (c *Config) Validate() error {
	var errs []error
	if c.BaseURL == "" {
		errs = append(errs, errors.New("base_url is required"))
	} else if err := coreconfig.ValidateURL(c.BaseURL); err != nil {
		errs = append(errs, fmt.Errorf("base_url: %w", err))
	}
	errs = append(errs,
		c.StatusAPI.Validate(SectionStatusAPI),
		c.CDN.Validate(SectionCDN),
		coreconfig.LegacyProxyError(c.LegacyProxy, SectionStatusAPI),
	)
	return errors.Join(errs...)
}
