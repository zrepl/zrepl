package cmd

import (
	"crypto/tls"
	"net"

	"context"
	"github.com/jinzhu/copier"
	"github.com/pkg/errors"
	"github.com/problame/go-netssh"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/cmd/tlsconf"
	"github.com/zrepl/zrepl/config"
	"time"
)

type SSHStdinserverConnecter struct {
	Host                 string
	User                 string
	Port                 uint16
	IdentityFile         string
	TransportOpenCommand []string
	SSHCommand           string
	Options              []string
	dialTimeout          time.Duration
}

var _ streamrpc.Connecter = &SSHStdinserverConnecter{}

func parseSSHStdinserverConnecter(in config.SSHStdinserverConnect) (c *SSHStdinserverConnecter, err error) {

	c = &SSHStdinserverConnecter{
		Host:         in.Host,
		User:         in.User,
		Port:         in.Port,
		IdentityFile: in.IdentityFile,
		SSHCommand:   in.SSHCommand,
		Options:      in.Options,
		dialTimeout:  in.DialTimeout,
	}
	return

}

type netsshConnToConn struct{ *netssh.SSHConn }

var _ net.Conn = netsshConnToConn{}

func (netsshConnToConn) SetDeadline(dl time.Time) error      { return nil }
func (netsshConnToConn) SetReadDeadline(dl time.Time) error  { return nil }
func (netsshConnToConn) SetWriteDeadline(dl time.Time) error { return nil }

func (c *SSHStdinserverConnecter) Connect(dialCtx context.Context) (net.Conn, error) {

	var endpoint netssh.Endpoint
	if err := copier.Copy(&endpoint, c); err != nil {
		return nil, errors.WithStack(err)
	}
	dialCtx, dialCancel := context.WithTimeout(dialCtx, c.dialTimeout) // context.TODO tied to error handling below
	defer dialCancel()
	nconn, err := netssh.Dial(dialCtx, endpoint)
	if err != nil {
		if err == context.DeadlineExceeded {
			err = errors.Errorf("dial_timeout of %s exceeded", c.dialTimeout)
		}
		return nil, err
	}
	return netsshConnToConn{nconn}, nil
}

type TCPConnecter struct {
	Address string
	dialer  net.Dialer
}

func parseTCPConnecter(in config.TCPConnect) (*TCPConnecter, error) {
	dialer := net.Dialer{
		Timeout: in.DialTimeout,
	}

	return &TCPConnecter{in.Address, dialer}, nil
}

func (c *TCPConnecter) Connect(dialCtx context.Context) (conn net.Conn, err error) {
	return c.dialer.DialContext(dialCtx, "tcp", c.Address)
}

type TLSConnecter struct {
	Address   string
	dialer    net.Dialer
	tlsConfig *tls.Config
}

func parseTLSConnecter(in config.TLSConnect) (*TLSConnecter, error) {
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
	return tls.DialWithDialer(&c.dialer, "tcp", c.Address, c.tlsConfig)
}
