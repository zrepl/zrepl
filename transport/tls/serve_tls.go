package tls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/tlsconf"
	"github.com/zrepl/zrepl/transport"
)

type TLSListenerFactory struct {
	address          string
	clientCA         *x509.CertPool
	serverCert       tls.Certificate
	handshakeTimeout time.Duration
	clientCNs        map[string]struct{}
}

func TLSListenerFactoryFromConfig(c *config.Global, in *config.TLSServe) (transport.AuthenticatedListenerFactory, error) {

	address := in.Listen
	handshakeTimeout := in.HandshakeTimeout

	if in.Ca == "" || in.Cert == "" || in.Key == "" {
		return nil, errors.New("fields 'ca', 'cert' and 'key'must be specified")
	}

	clientCA, err := tlsconf.ParseCAFile(in.Ca)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse ca file")
	}

	serverCert, err := tls.LoadX509KeyPair(in.Cert, in.Key)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse cer/key pair")
	}

	clientCNs := make(map[string]struct{}, len(in.ClientCNs))
	for i, cn := range in.ClientCNs {
		if err := transport.ValidateClientIdentity(cn); err != nil {
			return nil, errors.Wrapf(err, "unsuitable client_cn #%d %q", i, cn)
		}
		// dupes are ok fr now
		clientCNs[cn] = struct{}{}
	}

	lf := func() (transport.AuthenticatedListener, error) {
		l, err := net.Listen("tcp", address)
		if err != nil {
			return nil, err
		}
		tcpL := l.(*net.TCPListener)
		tl := tlsconf.NewClientAuthListener(tcpL, clientCA, serverCert, handshakeTimeout)
		return &tlsAuthListener{tl, clientCNs}, nil
	}

	return lf, nil
}

type tlsAuthListener struct {
	*tlsconf.ClientAuthListener
	clientCNs map[string]struct{}
}

func (l tlsAuthListener) Accept(ctx context.Context) (*transport.AuthConn, error) {
	tcpConn, tlsConn, cn, err := l.ClientAuthListener.Accept()
	if err != nil {
		return nil, err
	}
	if _, ok := l.clientCNs[cn]; !ok {
		if dl, ok := ctx.Deadline(); ok {
			defer tlsConn.SetDeadline(time.Time{})
			tlsConn.SetDeadline(dl)
		}
		if err := tlsConn.Close(); err != nil {
			transport.GetLogger(ctx).WithError(err).Error("error closing connection with unauthorized common name")
		}
		return nil, fmt.Errorf("unauthorized client common name %q from %s", cn, tlsConn.RemoteAddr())
	}
	adaptor := newWireAdaptor(tlsConn, tcpConn)
	return transport.NewAuthConn(adaptor, cn), nil
}
