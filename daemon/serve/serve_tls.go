package serve

import (
	"crypto/tls"
	"crypto/x509"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/tlsconf"
	"net"
	"time"
	"context"
)

type TLSListenerFactory struct {
	address          string
	clientCA         *x509.CertPool
	serverCert       tls.Certificate
	handshakeTimeout time.Duration
}

func TLSListenerFactoryFromConfig(c *config.Global, in *config.TLSServe) (lf *TLSListenerFactory, err error) {
	lf = &TLSListenerFactory{
		address: in.Listen,
	}

	if in.Ca == "" || in.Cert == "" || in.Key == "" {
		return nil, errors.New("fields 'ca', 'cert' and 'key'must be specified")
	}

	lf.clientCA, err = tlsconf.ParseCAFile(in.Ca)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse ca file")
	}

	lf.serverCert, err = tls.LoadX509KeyPair(in.Cert, in.Key)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse cer/key pair")
	}

	return lf, nil
}

func (f *TLSListenerFactory) Listen() (AuthenticatedListener, error) {
	l, err := net.Listen("tcp", f.address)
	if err != nil {
		return nil, err
	}
	tl := tlsconf.NewClientAuthListener(l, f.clientCA, f.serverCert, f.handshakeTimeout)
	return tlsAuthListener{tl}, nil
}

type tlsAuthListener struct {
	*tlsconf.ClientAuthListener
}

func (l tlsAuthListener) Accept(ctx context.Context) (AuthenticatedConn, error) {
	c, cn, err := l.ClientAuthListener.Accept()
	if err != nil {
		return nil, err
	}
	return authConn{c, cn}, nil
}


