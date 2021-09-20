package tls

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/tlsconf"
	"github.com/zrepl/zrepl/transport"
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

	if fakeCertificateLoading {
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
		fmt.Fprintf(os.Stderr, "tls connecter error %T\n\t%s\n\t%v\n\t%#v\n\n", err, err, err, err)
		return nil, err
	}
	tcpConn := conn.(*net.TCPConn)
	tlsConn := tls.Client(conn, c.tlsConfig)
	return newWireAdaptor(tlsConn, tcpConn), nil
}
