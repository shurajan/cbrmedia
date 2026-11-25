package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Способ подключения (Connection.Connection).
const (
	// ConnectionDirect — как решит операционная система: маршрут по умолчанию, без прокси.
	ConnectionDirect = "direct"
	// ConnectionProxy — через HTTP-прокси Connection.Proxy.
	ConnectionProxy = "proxy"
	// ConnectionInterface — через сетевой интерфейс Connection.Interface (eth0, wg0…):
	// сокеты привязываются к нему (core/netdial), только Linux и macOS.
	ConnectionInterface = "interface"
)

// interfaceName — допустимое имя сетевого интерфейса (в Linux — до 15 символов).
var interfaceName = regexp.MustCompile(`^[A-Za-z0-9_.:@-]{1,15}$`)

// Connection — секция подключения к одному внешнему адресату. Нотация едина для всех
// конфигов служб: "telegram" в tbot.json, "status_api" и "cdn" в recorder.json,
// "status_api" в api.json:
//
//	"cdn": {"connection": "proxy", "proxy": "http://127.0.0.1:8080", "interface": ""}
//	"cdn": {"connection": "interface", "proxy": "", "interface": "wg0"}
//
// Общих настроек нет: у каждого адресата своя секция. Прокси и интерфейс вместе
// не действуют — работает то, что выбрано в Connection.
type Connection struct {
	// Connection — ConnectionDirect (по умолчанию, в том числе без секции), ConnectionProxy
	// или ConnectionInterface.
	Connection string `json:"connection"`
	// Proxy — адрес HTTP-прокси; обязателен при ConnectionProxy, иначе не используется.
	Proxy string `json:"proxy"`
	// Interface — имя сетевого интерфейса; обязательно при ConnectionInterface, иначе не используется.
	Interface string `json:"interface"`
}

// Normalize убирает пробелы и подставляет ConnectionDirect.
func (c *Connection) Normalize() {
	c.Connection = strings.ToLower(strings.TrimSpace(c.Connection))
	if c.Connection == "" {
		c.Connection = ConnectionDirect
	}
	c.Proxy = strings.TrimSpace(c.Proxy)
	c.Interface = strings.TrimSpace(c.Interface)
}

// Validate проверяет секцию после Normalize; section — её имя в конфиге для текста ошибок.
// Есть ли интерфейс в системе, здесь не проверяется — это делает core/netdial при старте службы.
func (c Connection) Validate(section string) error {
	var errs []error
	switch c.Connection {
	case ConnectionDirect:
	case ConnectionProxy:
		if c.Proxy == "" {
			errs = append(errs, fmt.Errorf(`%s.proxy is required when %s.connection is %q`, section, section, ConnectionProxy))
		}
	case ConnectionInterface:
		if c.Interface == "" {
			errs = append(errs, fmt.Errorf(`%s.interface is required when %s.connection is %q`, section, section, ConnectionInterface))
		}
	default:
		errs = append(errs, fmt.Errorf("%s.connection: %q, want %q, %q or %q",
			section, c.Connection, ConnectionDirect, ConnectionProxy, ConnectionInterface))
	}
	if c.Proxy != "" {
		if err := ValidateURL(c.Proxy); err != nil {
			errs = append(errs, fmt.Errorf("%s.proxy: %w", section, err))
		}
	}
	if c.Interface != "" && !interfaceName.MatchString(c.Interface) {
		errs = append(errs, fmt.Errorf("%s.interface: %q is not an interface name", section, c.Interface))
	}
	return errors.Join(errs...)
}

// ProxyURL — адрес прокси для HTTP-клиента; пусто — без прокси.
func (c Connection) ProxyURL() string {
	if c.Connection != ConnectionProxy {
		return ""
	}
	return c.Proxy
}

// InterfaceName — интерфейс, к которому привязывать сокеты; пусто — не привязывать.
func (c Connection) InterfaceName() string {
	if c.Connection != ConnectionInterface {
		return ""
	}
	return c.Interface
}

// Value — подключение одним значением, как его принимает SetValue: ConnectionDirect,
// адрес прокси или имя интерфейса.
func (c Connection) Value() string {
	switch {
	case c.Connection == ConnectionProxy && c.Proxy != "":
		return c.Proxy
	case c.Connection == ConnectionInterface && c.Interface != "":
		return c.Interface
	default:
		return ConnectionDirect
	}
}

// SetValue задаёт подключение одним значением (флаги и диалог "<служба> config", флаги hlsprobe):
//
//   - "direct" — напрямую; сохранённые адрес прокси и имя интерфейса остаются в секции;
//   - адрес со схемой ("http://127.0.0.1:8080") — через этот HTTP-прокси;
//   - имя ("wg0") — через этот сетевой интерфейс;
//   - "proxy" / "interface" — снова включить сохранённый адрес прокси / интерфейс;
//   - пусто — напрямую, сохранённое стирается.
func (c *Connection) SetValue(value string) error {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "":
		*c = Connection{Connection: ConnectionDirect}
	case ConnectionDirect:
		c.Connection = ConnectionDirect
	case ConnectionProxy:
		if c.Proxy == "" {
			return errors.New("no proxy address is saved yet: pass the proxy URL")
		}
		c.Connection = ConnectionProxy
	case ConnectionInterface:
		if c.Interface == "" {
			return errors.New("no interface is saved yet: pass the interface name")
		}
		c.Connection = ConnectionInterface
	default:
		if strings.Contains(value, "://") {
			if err := ValidateURL(value); err != nil {
				return err
			}
			c.Connection, c.Proxy = ConnectionProxy, value
			return nil
		}
		if !interfaceName.MatchString(value) {
			return fmt.Errorf("%q is neither %q, a proxy URL nor an interface name", value, ConnectionDirect)
		}
		c.Connection, c.Interface = ConnectionInterface, value
	}
	return nil
}

// MigrateLegacyProxy переносит адрес из ключа "proxy" верхнего уровня (нотация до 1.4.1)
// в секцию c, если прокси в ней не настроен, и очищает старый ключ. Вызывается после
// Normalize подкомандой "<служба> config"; возвращает заметку для пользователя или пусто.
func MigrateLegacyProxy(legacy *string, c *Connection, section string) string {
	addr := strings.TrimSpace(*legacy)
	*legacy = ""
	switch {
	case addr == "":
		return ""
	case c.Proxy != "" || c.Connection == ConnectionProxy:
		return fmt.Sprintf(`old top-level "proxy" is dropped: %q is already configured`, section)
	}
	c.Connection, c.Proxy = ConnectionProxy, addr
	return fmt.Sprintf(`old top-level "proxy" is moved to %q`, section)
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
