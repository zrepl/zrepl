package connecter

import (
	"context"
	"github.com/zrepl/zrepl/config"
	"net"
)

type TCPConnecter struct {
	Address string
	dialer  net.Dialer
}

func TCPConnecterFromConfig(in *config.TCPConnect) (*TCPConnecter, error) {
	dialer := net.Dialer{
		Timeout: in.DialTimeout,
	}

	return &TCPConnecter{in.Address, dialer}, nil
}

func (c *TCPConnecter) Connect(dialCtx context.Context) (conn net.Conn, err error) {
	return c.dialer.DialContext(dialCtx, "tcp", c.Address)
}
