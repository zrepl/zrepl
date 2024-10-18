package tcpsock

import (
	"context"
	"net"
	"syscall"
)

func Listen(address string, tryFreeBind bool) (*net.TCPListener, error) {
	control := func(network, address string, c syscall.RawConn) error {
		if tryFreeBind {
			if err := freeBind(network, address, c); err != nil {
				return err
			}
		}
		return nil
	}
	var listenConfig = net.ListenConfig{
		Control: control,
	}

	l, err := listenConfig.Listen(context.Background(), "tcp", address)
	if err != nil {
		return nil, err
	}
	return l.(*net.TCPListener), nil
}
