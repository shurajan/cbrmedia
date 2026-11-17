package config

import (
	"errors"
	"fmt"
	"strings"
)

// Способ подключения (Connection.Connection).
const (
	// ConnectionDirect — напрямую, без прокси (по умолчанию).
	ConnectionDirect = "direct"
	// ConnectionProxy — через HTTP-прокси Connection.Proxy.
	ConnectionProxy = "proxy"
)

// Connection — секция подключения к одному внешнему адресату. Нотация едина для всех
// конфигов служб: "telegram" в tbot.json, "status_api" и "cdn" в recorder.json,
// "status_api" в api.json:
//
//	"cdn": {"connection": "proxy", "proxy": "http://127.0.0.1:8080"}
//
// Общего прокси нет: у каждого адресата свой адрес.
type Connection struct {
	// Connection — ConnectionDirect (по умолчанию, в том числе без секции) или ConnectionProxy.
	Connection string `json:"connection"`
	// Proxy — адрес HTTP-прокси; обязателен при ConnectionProxy, при ConnectionDirect не используется.
	Proxy string `json:"proxy"`
}

// Normalize убирает пробелы и подставляет ConnectionDirect.
func (c *Connection) Normalize() {
	c.Connection = strings.ToLower(strings.TrimSpace(c.Connection))
	if c.Connection == "" {
		c.Connection = ConnectionDirect
	}
	c.Proxy = strings.TrimSpace(c.Proxy)
}

// Validate проверяет секцию после Normalize; section — её имя в конфиге для текста ошибок.
func (c Connection) Validate(section string) error {
	var errs []error
	switch c.Connection {
	case ConnectionDirect:
	case ConnectionProxy:
		if c.Proxy == "" {
			errs = append(errs, fmt.Errorf(`%s.proxy is required when %s.connection is %q`, section, section, ConnectionProxy))
		}
	default:
		errs = append(errs, fmt.Errorf("%s.connection: %q, want %q or %q", section, c.Connection, ConnectionDirect, ConnectionProxy))
	}
	if c.Proxy != "" {
		if err := ValidateURL(c.Proxy); err != nil {
			errs = append(errs, fmt.Errorf("%s.proxy: %w", section, err))
		}
	}
	return errors.Join(errs...)
}

// ProxyURL — адрес прокси для HTTP-клиента; пусто — прямое подключение.
func (c Connection) ProxyURL() string {
	if c.Connection != ConnectionProxy {
		return ""
	}
	return c.Proxy
}

// LegacyProxyError — ошибка для ключа "proxy" верхнего уровня (нотация до 1.4.1):
// молча он действовать перестать не должен, иначе трафик пойдёт напрямую.
// value — значение старого ключа, section — секция, куда перенести адрес.
// Пустой ключ (как в старых шаблонах) ошибкой не считается.
func LegacyProxyError(value, section string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return fmt.Errorf(`top-level "proxy" is no longer supported: move the address to "%s": {"connection": %q, "proxy": "..."}`,
		section, ConnectionProxy)
}
