//go:build linux
// +build linux

package tcpsock

import (
	"syscall"
)

func freeBind(network, address string, c syscall.RawConn) error {
	var err, sockerr error
	err = c.Control(func(fd uintptr) {
		// apparently, this works for both IPv4 and IPv6
		sockerr = syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_FREEBIND, 1)
	})
	if err != nil {
		return err
	}
	return sockerr
}
