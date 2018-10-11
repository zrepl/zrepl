package serve

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
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
	clientCNs map[string]struct{}
}

func TLSListenerFactoryFromConfig(c *config.Global, in *config.TLSServe) (lf *TLSListenerFactory, err error) {
	lf = &TLSListenerFactory{
		address: in.Listen,
		handshakeTimeout: in.HandshakeTimeout,
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

	lf.clientCNs = make(map[string]struct{}, len(in.ClientCNs))
	for i, cn := range in.ClientCNs {
		if err := ValidateClientIdentity(cn); err != nil {
			return nil, errors.Wrapf(err, "unsuitable client_cn #%d %q", i, cn)
		}
		// dupes are ok fr now
		lf.clientCNs[cn] = struct{}{}
	}

	return lf, nil
}

func (f *TLSListenerFactory) Listen() (AuthenticatedListener, error) {
	l, err := net.Listen("tcp", f.address)
	if err != nil {
		return nil, err
	}
	tl := tlsconf.NewClientAuthListener(l, f.clientCA, f.serverCert, f.handshakeTimeout)
	return tlsAuthListener{tl, f.clientCNs}, nil
}

type tlsAuthListener struct {
	*tlsconf.ClientAuthListener
	clientCNs map[string]struct{}
}

func (l tlsAuthListener) Accept(ctx context.Context) (AuthenticatedConn, error) {
	c, cn, err := l.ClientAuthListener.Accept()
	if err != nil {
		return nil, err
	}
	if _, ok := l.clientCNs[cn]; !ok {
		if err := c.Close(); err != nil {
			getLogger(ctx).WithError(err).Error("error closing connection with unauthorized common name")
		}
		return nil, fmt.Errorf("unauthorized client common name %q from %s", cn, c.RemoteAddr())
	}
	return authConn{c, cn}, nil
}


