package connecter

import (
	"context"
	"github.com/jinzhu/copier"
	"github.com/pkg/errors"
	"github.com/problame/go-netssh"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/config"
	"net"
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

func SSHStdinserverConnecterFromConfig(in *config.SSHStdinserverConnect) (c *SSHStdinserverConnecter, err error) {

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
