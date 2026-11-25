// Package netdial настраивает исходящие соединения по секции подключения конфига
// (core/config.Connection): напрямую, через HTTP-прокси или через заданный сетевой
// интерфейс (eth0, wg0…).
//
// Привязка к интерфейсу — на уровне сокета: SO_BINDTODEVICE в Linux, IP_BOUND_IF /
// IPV6_BOUND_IF в macOS; на других системах такое подключение — ошибка при старте.
// Привязываются только соединения HTTP-клиента: имена хостов разрешает системный
// резолвер, DNS-запросы идут обычным маршрутом.
package netdial

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"

	coreconfig "cbr/core/config"
)

// Configure настраивает transport по секции c: прокси — в transport.Proxy, интерфейс —
// в dialer.Control; transport.DialContext всегда берётся из dialer. Переменные
// окружения HTTP(S)_PROXY не действуют: подключение определяет только конфиг.
//
// Интерфейс проверяется сразу: если его нет, служба не стартует, а не уходит
// молча на маршрут по умолчанию. Если он пропадёт позже, соединения через него
// будут завершаться ошибкой.
func Configure(transport *http.Transport, dialer *net.Dialer, c coreconfig.Connection) error {
	transport.Proxy = nil
	if proxy := c.ProxyURL(); proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return fmt.Errorf("invalid proxy URL %q: %w", proxy, err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	if iface := c.InterfaceName(); iface != "" {
		if !bindSupported {
			return fmt.Errorf("connection through interface %q is not supported on this OS (Linux and macOS only)", iface)
		}
		if err := CheckInterface(iface); err != nil {
			return err
		}
		dialer.Control = func(network, _ string, conn syscall.RawConn) error {
			if err := bindToInterface(network, iface, conn); err != nil {
				return fmt.Errorf("bind to interface %s: %w", iface, err)
			}
			return nil
		}
	}
	transport.DialContext = dialer.DialContext
	return nil
}

// Describe — подключение для лога: способ и адрес прокси или имя интерфейса.
func Describe(c coreconfig.Connection) (kind, target string) {
	switch {
	case c.ProxyURL() != "":
		return coreconfig.ConnectionProxy, c.ProxyURL()
	case c.InterfaceName() != "":
		return coreconfig.ConnectionInterface, c.InterfaceName()
	default:
		return coreconfig.ConnectionDirect, ""
	}
}

// CheckInterface проверяет, что в системе есть сетевой интерфейс name; в тексте
// ошибки — список имеющихся.
func CheckInterface(name string) error {
	if _, err := net.InterfaceByName(name); err == nil {
		return nil
	}
	var names []string
	if all, err := net.Interfaces(); err == nil {
		for _, ifi := range all {
			names = append(names, ifi.Name)
		}
	}
	return fmt.Errorf("no network interface %q (available: %s)", name, strings.Join(names, ", "))
}

// LoopbackInterface — имя поднятого loopback-интерфейса (lo, lo0) или пусто; нужен тестам,
// которым требуется существующий интерфейс.
func LoopbackInterface() string {
	all, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifi := range all {
		if ifi.Flags&net.FlagLoopback != 0 && ifi.Flags&net.FlagUp != 0 {
			return ifi.Name
		}
	}
	return ""
}
