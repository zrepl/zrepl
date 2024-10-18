package tls

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/internal/config"
	"github.com/zrepl/zrepl/internal/tlsconf"
	"github.com/zrepl/zrepl/internal/transport"
)

type TLSConnecter struct {
	Address   string
	dialer    net.Dialer
	tlsConfig *tls.Config
}

func TLSConnecterFromConfig(in *config.TLSConnect, parseFlags config.ParseFlags) (*TLSConnecter, error) {
	dialer := net.Dialer{
		Timeout: in.DialTimeout,
	}

	if parseFlags&config.ParseFlagsNoCertCheck != 0 {
		return &TLSConnecter{in.Address, dialer, nil}, nil
	}

	ca, err := tlsconf.ParseCAFile(in.Ca)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse ca file")
	}

	cert, err := tls.LoadX509KeyPair(in.Cert, in.Key)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse cert/key pair")
	}

	tlsConfig, err := tlsconf.ClientAuthClient(in.ServerCN, ca, cert)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build tls config")
	}

	return &TLSConnecter{in.Address, dialer, tlsConfig}, nil
}

func (c *TLSConnecter) Connect(dialCtx context.Context) (transport.Wire, error) {
	conn, err := c.dialer.DialContext(dialCtx, "tcp", c.Address)
	if err != nil {
		return nil, err
	}
	tcpConn := conn.(*net.TCPConn)
	tlsConn := tls.Client(conn, c.tlsConfig)
	return newWireAdaptor(tlsConn, tcpConn), nil
}
