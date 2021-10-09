//go:build !linux && !freebsd
// +build !linux,!freebsd

package tcpsock

import (
	"fmt"
	"syscall"
)

func freeBind(network, address string, c syscall.RawConn) error {
	return fmt.Errorf("IP_FREEBIND equivalent functionality not supported  on this platform")
}
