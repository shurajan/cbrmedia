// Package config — настройки api: ~/.cbr/config/api.json (необязательный файл).
package config

import (
	"errors"
	"io/fs"
	"strings"

	coreconfig "cbr/core/config"
)

// Name — имя службы и файла конфига.
const Name = "api"

// DefaultAddr — адрес HTTP API по умолчанию.
const DefaultAddr = ":8000"

// Config — содержимое api.json.
type Config struct {
	// Addr — адрес для прослушивания, например ":8000".
	Addr string `json:"addr"`
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

// Normalize подставляет значения по умолчанию.
func (c *Config) Normalize() {
	c.Addr = strings.TrimSpace(c.Addr)
	if c.Addr == "" {
		c.Addr = DefaultAddr
	}
}

// Validate проверяет конфиг.
func (c *Config) Validate() error { return nil }
