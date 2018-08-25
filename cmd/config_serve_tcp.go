package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/cmd/tlsconf"
)

type TCPListenerFactory struct {
	Address          string
	tls              bool
	clientCA         *x509.CertPool
	serverCert       tls.Certificate
	clientCommonName string
}

func parseTCPListenerFactory(c JobParsingContext, i map[string]interface{}) (*TCPListenerFactory, error) {

	var in struct {
		Address string
		TLS     map[string]interface{}
	}
	if err := mapstructure.Decode(i, &in); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}

	lf := &TCPListenerFactory{}

	if in.Address == "" {
		return nil, errors.New("must specify field 'address'")
	}
	lf.Address = in.Address

	if in.TLS != nil {
		err := func(i map[string]interface{}) (err error) {
			var in struct {
				CA       string
				Cert     string
				Key      string
				ClientCN string `mapstructure:"client_cn"`
			}
			if err := mapstructure.Decode(i, &in); err != nil {
				return errors.Wrap(err, "mapstructure error")
			}

			if in.CA == "" || in.Cert == "" || in.Key == "" || in.ClientCN == "" {
				return errors.New("fields 'ca', 'cert', 'key' and 'client_cn' must be specified")
			}

			lf.clientCommonName = in.ClientCN

			lf.clientCA, err = tlsconf.ParseCAFile(in.CA)
			if err != nil {
				return errors.Wrap(err, "cannot parse ca file")
			}

			lf.serverCert, err = tls.LoadX509KeyPair(in.Cert, in.Key)
			if err != nil {
				return errors.Wrap(err, "cannot parse cer/key pair")
			}

			lf.tls = true // mark success
			return nil
		}(in.TLS)
		if err != nil {
			return nil, errors.Wrap(err, "error parsing TLS config in field 'tls'")
		}
	}

	return lf, nil
}

var TCPListenerHandshakeTimeout = 10 * time.Second // FIXME make configurable

func (f *TCPListenerFactory) Listen() (net.Listener, error) {
	l, err := net.Listen("tcp", f.Address)
	if !f.tls || err != nil {
		return l, err
	}

	tl := tlsconf.NewClientAuthListener(l, f.clientCA, f.serverCert, f.clientCommonName, TCPListenerHandshakeTimeout)
	return tl, nil
}
