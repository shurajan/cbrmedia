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

// Способ подключения к CDN (CDN.Connection).
const (
	// ConnectionDirect — напрямую, без прокси (по умолчанию).
	ConnectionDirect = "direct"
	// ConnectionProxy — через прокси CDN.Proxy, а если он пуст — через общий Proxy.
	ConnectionProxy = "proxy"
)

// Config — содержимое recorder.json.
type Config struct {
	// BaseURL — базовый адрес status API; пользователь опрашивается по <BaseURL>/<user>/.
	BaseURL string `json:"base_url"`
	// Proxy — HTTP-прокси для запросов к status API (необязательно). На CDN не действует.
	Proxy string `json:"proxy"`
	// CDN — подключение к CDN, с которого качаются плейлисты и сегменты (hls_source).
	CDN CDN `json:"cdn"`
}

// CDN — секция "cdn" в recorder.json.
type CDN struct {
	// Connection — ConnectionDirect (по умолчанию, в том числе без секции) или ConnectionProxy.
	Connection string `json:"connection"`
	// Proxy — HTTP-прокси для CDN при Connection = ConnectionProxy; пусто — общий Config.Proxy.
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

// Normalize убирает пробелы по краям и подставляет значения по умолчанию.
func (c *Config) Normalize() {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.Proxy = strings.TrimSpace(c.Proxy)
	c.CDN.Connection = strings.ToLower(strings.TrimSpace(c.CDN.Connection))
	if c.CDN.Connection == "" {
		c.CDN.Connection = ConnectionDirect
	}
	c.CDN.Proxy = strings.TrimSpace(c.CDN.Proxy)
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
	switch c.CDN.Connection {
	case ConnectionDirect:
	case ConnectionProxy:
		if c.CDN.Proxy == "" && c.Proxy == "" {
			errs = append(errs, errors.New(`cdn.connection is "proxy", but neither cdn.proxy nor proxy is set`))
		}
	default:
		errs = append(errs, fmt.Errorf("cdn.connection: %q, want %q or %q", c.CDN.Connection, ConnectionDirect, ConnectionProxy))
	}
	if c.CDN.Proxy != "" {
		if err := coreconfig.ValidateURL(c.CDN.Proxy); err != nil {
			errs = append(errs, fmt.Errorf("cdn.proxy: %w", err))
		}
	}
	return errors.Join(errs...)
}

// CDNProxy — прокси для CDN; пусто — прямое подключение.
func (c *Config) CDNProxy() string {
	if c.CDN.Connection != ConnectionProxy {
		return ""
	}
	if c.CDN.Proxy != "" {
		return c.CDN.Proxy
	}
	return c.Proxy
}
