// Package config — настройки tbot: ~/.cbr/config/tbot.json.
package config

import (
	"errors"
	"fmt"
	"strings"

	coreconfig "cbr/core/config"
)

// Name — имя службы и файла конфига.
const Name = "tbot"

// Config — содержимое tbot.json.
type Config struct {
	// BotToken — токен бота от @BotFather.
	BotToken string `json:"bot_token"`
	// AllowedUserID — единственный пользователь Telegram, которому разрешены команды.
	AllowedUserID int64 `json:"allowed_user_id"`
	// Proxy — HTTP-прокси для Telegram API (необязательно).
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
	c.BotToken = strings.TrimSpace(c.BotToken)
	c.Proxy = strings.TrimSpace(c.Proxy)
}

// Validate проверяет обязательные поля.
func (c *Config) Validate() error {
	var errs []error
	if c.BotToken == "" {
		errs = append(errs, errors.New("bot_token is required"))
	}
	if c.AllowedUserID == 0 {
		errs = append(errs, errors.New("allowed_user_id is required"))
	}
	if c.Proxy != "" {
		if err := coreconfig.ValidateURL(c.Proxy); err != nil {
			errs = append(errs, fmt.Errorf("proxy: %w", err))
		}
	}
	return errors.Join(errs...)
}
