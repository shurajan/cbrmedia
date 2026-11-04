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

// Config — содержимое recorder.json.
type Config struct {
	// BaseURL — базовый адрес status API; пользователь опрашивается по <BaseURL>/<user>/.
	BaseURL string `json:"base_url"`
	// Proxy — HTTP-прокси для опроса и скачивания стримов (необязательно).
	Proxy string `json:"proxy"`
}

// Load читает и проверяет конфиг.
func Load(path string) (*Config, error) {
	var cfg Config
	if err := coreconfig.Load(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Normalize убирает пробелы по краям.
func (c *Config) Normalize() {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.Proxy = strings.TrimSpace(c.Proxy)
}

// Validate проверяет обязательные поля и корректность URL.
func (c *Config) Validate() error {
	var errs []error
	if c.BaseURL == "" {
		errs = append(errs, errors.New("base_url is required"))
	} else if err := coreconfig.ValidateURL(c.BaseURL); err != nil {
		errs = append(errs, fmt.Errorf("base_url: %w", err))
	}
	if c.Proxy != "" {
		if err := coreconfig.ValidateURL(c.Proxy); err != nil {
			errs = append(errs, fmt.Errorf("proxy: %w", err))
		}
	}
	return errors.Join(errs...)
}
