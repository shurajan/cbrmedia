package netdial

import (
	"net"
	"strings"
	"syscall"
)

const bindSupported = true

// bindToInterface привязывает сокет к интерфейсу через IP_BOUND_IF / IPV6_BOUND_IF.
// Индекс ищется при каждом соединении: у пересозданного интерфейса (VPN) он другой.
func bindToInterface(network, iface string, conn syscall.RawConn) error {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return err
	}
	level, opt := syscall.IPPROTO_IP, syscall.IP_BOUND_IF
	if strings.HasSuffix(network, "6") {
		level, opt = syscall.IPPROTO_IPV6, syscall.IPV6_BOUND_IF
	}
	var bindErr error
	if err := conn.Control(func(fd uintptr) {
		bindErr = syscall.SetsockoptInt(int(fd), level, opt, ifi.Index)
	}); err != nil {
		return err
	}
	return bindErr
}
