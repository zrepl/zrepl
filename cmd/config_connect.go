package cmd

import (
	"fmt"
	"net"

	"context"
	"github.com/jinzhu/copier"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/problame/go-netssh"
	"github.com/problame/go-streamrpc"
	"time"
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
