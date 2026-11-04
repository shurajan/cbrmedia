// Package config — общие помощники для конфигов служб: у каждой службы свой
// файл ~/.cbr/config/<служба>.json со своей структурой.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
)

// Validator — конфиг, который умеет подставлять значения по умолчанию и проверять себя.
type Validator interface {
	Normalize()
	Validate() error
}

// Load читает JSON из path в cfg, применяет Normalize и проверяет Validate.
// Неизвестные поля игнорируются.
func Load(path string, cfg Validator) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config %s: %w", path, err)
	}
	return nil
}

// ValidateURL проверяет, что raw — абсолютный URL со схемой и хостом.
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%q must be an absolute URL", raw)
	}
	return nil
}
