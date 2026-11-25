package netdial

import "syscall"

const bindSupported = true

// bindToInterface привязывает сокет к интерфейсу через SO_BINDTODEVICE. Обычному
// пользователю это разрешено с ядра Linux 5.7; на более старых нужен CAP_NET_RAW.
func bindToInterface(_, iface string, conn syscall.RawConn) error {
	var bindErr error
	if err := conn.Control(func(fd uintptr) {
		bindErr = syscall.BindToDevice(int(fd), iface)
	}); err != nil {
		return err
	}
	return bindErr
}
