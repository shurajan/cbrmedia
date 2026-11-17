// Package config — настройки recorder: ~/.cbr/config/recorder.json.
package config

import (
	"errors"
	"fmt"
	"strings"

	coreconfig "cbr/core/config"
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
	LegacyProxy string `json:"proxy"`
}

// Load читает и проверяет конфиг.
func Load(path string) (*Config, error) {
	var cfg Config
	if err := coreconfig.Load(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
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
