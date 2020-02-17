package tls

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/tlsconf"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/util/tcpsock"
)

type TLSListenerFactory struct{}

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
		return nil, errors.Wrap(err, "cannot parse cert/key pair")
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
		l, err := tcpsock.Listen(address, in.ListenFreeBind)
		if err != nil {
			return nil, err
		}
		tl := tlsconf.NewClientAuthListener(l, clientCA, serverCert, handshakeTimeout)
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
		log := transport.GetLogger(ctx)
		if dl, ok := ctx.Deadline(); ok {
			defer func() {
				err := tlsConn.SetDeadline(time.Time{})
				if err != nil {
					log.WithError(err).Error("cannot clear connection deadline")
				}
			}()
			err := tlsConn.SetDeadline(dl)
			if err != nil {
				log.WithError(err).WithField("deadline", dl).Error("cannot set connection deadline inherited from context")
			}
		}
		if err := tlsConn.Close(); err != nil {
			log.WithError(err).Error("error closing connection with unauthorized common name")
		}
		return nil, fmt.Errorf("unauthorized client common name %q from %s", cn, tlsConn.RemoteAddr())
	}
	adaptor := newWireAdaptor(tlsConn, tcpConn)
	return transport.NewAuthConn(adaptor, cn), nil
}
