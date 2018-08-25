package cmd

import (
	"crypto/tls"
	"fmt"
	"net"

	"context"
	"github.com/jinzhu/copier"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/problame/go-netssh"
	"github.com/problame/go-streamrpc"
	"time"
	"github.com/zrepl/zrepl/cmd/tlsconf"
)

type SSHStdinserverConnecter struct {
	Host                 string
	User                 string
	Port                 uint16
	IdentityFile         string   `mapstructure:"identity_file"`
	TransportOpenCommand []string `mapstructure:"transport_open_command"`
	SSHCommand           string   `mapstructure:"ssh_command"`
	Options              []string
	DialTimeout          string `mapstructure:"dial_timeout"`
	dialTimeout          time.Duration
}

var _ streamrpc.Connecter = &SSHStdinserverConnecter{}

func parseSSHStdinserverConnecter(i map[string]interface{}) (c *SSHStdinserverConnecter, err error) {

	c = &SSHStdinserverConnecter{}
	if err = mapstructure.Decode(i, c); err != nil {
		err = errors.New(fmt.Sprintf("could not parse ssh transport: %s", err))
		return nil, err
	}

	if c.DialTimeout != "" {
		c.dialTimeout, err = time.ParseDuration(c.DialTimeout)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse dial_timeout")
		}
	} else {
		c.dialTimeout = 10 * time.Second
	}

	// TODO assert fields are filled
	return

}

type netsshConnToConn struct { *netssh.SSHConn }

var _ net.Conn = netsshConnToConn{}

func (netsshConnToConn) SetDeadline(dl time.Time) error { return nil }
func (netsshConnToConn) SetReadDeadline(dl time.Time) error { return nil }
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
	Host      string
	Port      uint16
	dialer    net.Dialer
	tlsConfig *tls.Config
}

func parseTCPConnecter(i map[string]interface{}) (*TCPConnecter, error) {
	var in struct {
		Host        string
		Port        uint16
		DialTimeout string `mapstructure:"dial_timeout"`
		TLS         map[string]interface{}
	}
	if err := mapstructure.Decode(i, &in); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}

	if in.Host == "" || in.Port == 0 {
		return nil, errors.New("fields 'host' and 'port' must not be empty")
	}
	dialTimeout, err := parsePostitiveDuration(in.DialTimeout)
	if err != nil {
		if in.DialTimeout != "" {
			return nil, errors.Wrap(err, "cannot parse field 'dial_timeout'")
		}
		dialTimeout = 10 * time.Second
	}
	dialer := net.Dialer{
		Timeout: dialTimeout,
	}

	var tlsConfig *tls.Config
	if in.TLS != nil {
		tlsConfig, err = func(i map[string]interface{}) (config *tls.Config, err error) {
			var in struct {
				CA       string
				Cert     string
				Key      string
				ServerCN string `mapstructure:"server_cn"`
			}
			if err := mapstructure.Decode(i, &in); err != nil {
				return nil, errors.Wrap(err, "mapstructure error")
			}
			if in.CA == "" || in.Cert == "" || in.Key == "" || in.ServerCN == "" {
				return nil, errors.New("fields 'ca', 'cert', 'key' and 'server_cn' must be specified")
			}

			ca, err := tlsconf.ParseCAFile(in.CA)
			if err != nil {
				return nil, errors.Wrap(err, "cannot parse ca file")
			}

			cert, err := tls.LoadX509KeyPair(in.Cert, in.Key)
			if err != nil {
				return nil, errors.Wrap(err, "cannot parse cert/key pair")
			}

			return tlsconf.ClientAuthClient(in.ServerCN, ca, cert)
		}(in.TLS)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse TLS config in field 'tls'")
		}
	}

	return &TCPConnecter{in.Host, in.Port, dialer, tlsConfig}, nil
}

func (c *TCPConnecter) Connect(dialCtx context.Context) (conn net.Conn, err error) {
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	if c.tlsConfig != nil {
		return tls.DialWithDialer(&c.dialer, "tcp", addr, c.tlsConfig)
	}
	return c.dialer.DialContext(dialCtx, "tcp", addr)
}
