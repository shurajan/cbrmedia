// Package config — настройки tbot: ~/.cbr/config/tbot.json.
package config

import (
	"errors"
	"strings"

	coreconfig "cbr/core/config"
)

// Name — имя службы и файла конфига.
const Name = "tbot"

// SectionTelegram — имя секции подключения в tbot.json.
const SectionTelegram = "telegram"

// Config — содержимое tbot.json.
type Config struct {
	// BotToken — токен бота от @BotFather.
	BotToken string `json:"bot_token"`
	// AllowedUserID — единственный пользователь Telegram, которому разрешены команды.
	AllowedUserID int64 `json:"allowed_user_id"`
	// Telegram — подключение к Telegram API.
	Telegram coreconfig.Connection `json:"telegram"`
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
	c.BotToken = strings.TrimSpace(c.BotToken)
	c.Telegram.Normalize()
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
	errs = append(errs,
		c.Telegram.Validate(SectionTelegram),
		coreconfig.LegacyProxyError(c.LegacyProxy, SectionTelegram),
	)
	return errors.Join(errs...)
}
