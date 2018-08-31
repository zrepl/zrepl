package serve

import (
	"crypto/tls"
	"crypto/x509"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/tlsconf"
	"net"
	"time"
)

type TLSListenerFactory struct {
	address          string
	clientCA         *x509.CertPool
	serverCert       tls.Certificate
	clientCommonName string
	handshakeTimeout time.Duration
}

func TLSListenerFactoryFromConfig(c *config.Global, in *config.TLSServe) (lf *TLSListenerFactory, err error) {
	lf = &TLSListenerFactory{
		address: in.Listen,
	}

	if in.Ca == "" || in.Cert == "" || in.Key == "" || in.ClientCN == "" {
		return nil, errors.New("fields 'ca', 'cert', 'key' and 'client_cn' must be specified")
	}

	lf.clientCommonName = in.ClientCN

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

func (f *TLSListenerFactory) Listen() (net.Listener, error) {
	l, err := net.Listen("tcp", f.address)
	if err != nil {
		return nil, err
	}
	tl := tlsconf.NewClientAuthListener(l, f.clientCA, f.serverCert, f.clientCommonName, f.handshakeTimeout)
	return tl, nil
}
