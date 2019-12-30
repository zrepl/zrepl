// +build freebsd

package tcpsock

import (
	"fmt"
	"syscall"
)

func freeBind(network, address string, c syscall.RawConn) error {
	var err, sockerr error
	err = c.Control(func(fd uintptr) {
		if network == "tcp6" {
			sockerr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_BINDANY, 1)
		} else if network == "tcp4" {
			sockerr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_BINDANY, 1)
		} else {
			sockerr = fmt.Errorf("expecting 'tcp6' or 'tcp4', got %q", network)
		}
	})
	if err != nil {
		return err
	}
	return sockerr
}
