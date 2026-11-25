//go:build !linux && !darwin

package netdial

import (
	"errors"
	"syscall"
)

const bindSupported = false

func bindToInterface(_, _ string, _ syscall.RawConn) error {
	return errors.New("binding to a network interface is not supported on this OS")
}
