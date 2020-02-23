package tlsconf

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
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
	l                *net.TCPListener
	c                *tls.Config
	handshakeTimeout time.Duration
}

func NewClientAuthListener(
	l *net.TCPListener, ca *x509.CertPool, serverCert tls.Certificate,
	handshakeTimeout time.Duration) *ClientAuthListener {

	if ca == nil {
		panic(ca)
	}
	if serverCert.Certificate == nil || serverCert.PrivateKey == nil {
		panic(serverCert)
	}

	tlsConf := &tls.Config{
		Certificates:             []tls.Certificate{serverCert},
		ClientCAs:                ca,
		ClientAuth:               tls.RequireAndVerifyClientCert,
		PreferServerCipherSuites: true,
		KeyLogWriter:             keylogFromEnv(),
	}
	return &ClientAuthListener{
		l,
		tlsConf,
		handshakeTimeout,
	}
}

// Accept() accepts a connection from the *net.TCPListener passed to the constructor
// and sets up the TLS connection, including handshake and peer CommonName validation
// within the specified handshakeTimeout.
//
// It returns both the raw TCP connection (tcpConn) and the TLS connection (tlsConn) on top of it.
// Access to the raw tcpConn might be necessary if CloseWrite semantics are desired:
// tlsConn.CloseWrite does NOT call tcpConn.CloseWrite, hence we provide access to tcpConn to
// allow the caller to do this by themselves.
func (l *ClientAuthListener) Accept() (tcpConn *net.TCPConn, tlsConn *tls.Conn, clientCN string, err error) {
	tcpConn, err = l.l.AcceptTCP()
	if err != nil {
		return nil, nil, "", err
	}

	tlsConn = tls.Server(tcpConn, l.c)
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
	if err = tlsConn.SetDeadline(time.Time{}); err != nil {
		goto CloseAndErr
	}

	peerCerts = tlsConn.ConnectionState().PeerCertificates
	if len(peerCerts) < 1 {
		err = errors.New("client must present full RFC5246:7.4.2 TLS client certificate chain")
		goto CloseAndErr
	}
	cn = peerCerts[0].Subject.CommonName
	return tcpConn, tlsConn, cn, nil
CloseAndErr:
	// unlike CloseWrite, Close on *tls.Conn actually closes the underlying connection
	tlsConn.Close() // TODO log error
	return nil, nil, "", err
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
		KeyLogWriter: keylogFromEnv(),
	}
	tlsConfig.BuildNameToCertificate()
	return tlsConfig, nil
}

func keylogFromEnv() io.Writer {
	var keyLog io.Writer = nil
	if outfile := os.Getenv("ZREPL_KEYLOG_FILE"); outfile != "" {
		fmt.Fprintf(os.Stderr, "writing to key log %s\n", outfile)
		var err error
		keyLog, err = os.OpenFile(outfile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
	}
	return keyLog
}
