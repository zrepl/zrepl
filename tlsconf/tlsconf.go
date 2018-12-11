package tlsconf

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io/ioutil"
	"net"
	"time"
)

func ParseCAFile(certfile string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	pem, err := ioutil.ReadFile(certfile)
	if err != nil {
		return nil, err
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("PEM parsing error")
	}
	return pool, nil
}

type ClientAuthListener struct {
	l                net.Listener
	handshakeTimeout time.Duration
}

func NewClientAuthListener(
	l net.Listener, ca *x509.CertPool, serverCert tls.Certificate,
	handshakeTimeout time.Duration) *ClientAuthListener {

	if ca == nil {
		panic(ca)
	}
	if serverCert.Certificate == nil || serverCert.PrivateKey == nil {
		panic(serverCert)
	}

	tlsConf := tls.Config{
		Certificates:             []tls.Certificate{serverCert},
		ClientCAs:                ca,
		ClientAuth:               tls.RequireAndVerifyClientCert,
		PreferServerCipherSuites: true,
	}
	l = tls.NewListener(l, &tlsConf)
	return &ClientAuthListener{
		l,
		handshakeTimeout,
	}
}

func (l *ClientAuthListener) Accept() (c net.Conn, clientCN string, err error) {
	c, err = l.l.Accept()
	if err != nil {
		return nil, "", err
	}
	tlsConn, ok := c.(*tls.Conn)
	if !ok {
		return c, "", err
	}

	var (
		cn        string
		peerCerts []*x509.Certificate
	)
	if err = tlsConn.SetDeadline(time.Now().Add(l.handshakeTimeout)); err != nil {
		goto CloseAndErr
	}
	if err = tlsConn.Handshake(); err != nil {
		goto CloseAndErr
	}
	tlsConn.SetDeadline(time.Time{})

	peerCerts = tlsConn.ConnectionState().PeerCertificates
	if len(peerCerts) != 1 {
		err = errors.New("unexpected number of certificates presented by TLS client")
		goto CloseAndErr
	}
	cn = peerCerts[0].Subject.CommonName
	return c, cn, nil
CloseAndErr:
	c.Close()
	return nil, "", err
}

func (l *ClientAuthListener) Addr() net.Addr {
	return l.l.Addr()
}

func (l *ClientAuthListener) Close() error {
	return l.l.Close()
}

func ClientAuthClient(serverName string, rootCA *x509.CertPool, clientCert tls.Certificate) (*tls.Config, error) {
	if serverName == "" {
		panic(serverName)
	}
	if rootCA == nil {
		panic(rootCA)
	}
	if clientCert.Certificate == nil || clientCert.PrivateKey == nil {
		panic(clientCert)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      rootCA,
		ServerName:   serverName,
	}
	tlsConfig.BuildNameToCertificate()
	return tlsConfig, nil
}
