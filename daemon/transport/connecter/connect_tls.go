package connecter

import (
	"context"
	"crypto/tls"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/tlsconf"
	"net"
)

type TLSConnecter struct {
	Address   string
	dialer    net.Dialer
	tlsConfig *tls.Config
}

func TLSConnecterFromConfig(in *config.TLSConnect) (*TLSConnecter, error) {
	dialer := net.Dialer{
		Timeout: in.DialTimeout,
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

func (c *TLSConnecter) Connect(dialCtx context.Context) (conn net.Conn, err error) {
	conn, err = c.dialer.DialContext(dialCtx, "tcp", c.Address)
	if err != nil {
		return nil, err
	}
	return tls.Client(conn, c.tlsConfig), nil
}
