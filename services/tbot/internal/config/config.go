// Package config — настройки tbot: ~/.cbr/config/tbot.json.
package config

import (
	"errors"
	"strings"

	coreconfig "cbr/core/config"
	"cbr/core/configcli"
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

// CLI — подкоманда "tbot config", которая пишет конфиг в path.
func CLI(path string) *configcli.Command {
	cfg := &Config{}
	return &configcli.Command{
		Service: Name,
		Path:    path,
		Config:  cfg,
		Params: []configcli.Param{
			configcli.Secret("bot-token", "bot_token", "<token>", "bot token from @BotFather", true, &cfg.BotToken),
			configcli.Int64("allowed-user-id", "allowed_user_id", "<id>", "the only Telegram user allowed to send commands", true, &cfg.AllowedUserID),
			configcli.Connection("telegram", SectionTelegram, "connection to the Telegram API", &cfg.Telegram),
		},
		Migrate: func() []string {
			if note := coreconfig.MigrateLegacyProxy(&cfg.LegacyProxy, &cfg.Telegram, SectionTelegram); note != "" {
				return []string{note}
			}
			return nil
		},
		Examples: []string{
			"config",
			"config --bot-token 123456:ABC... --allowed-user-id 123456789",
			"config --telegram http://127.0.0.1:8080",
			"config --telegram direct",
		},
	}
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
