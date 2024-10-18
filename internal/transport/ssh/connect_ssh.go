package ssh

import (
	"context"
	"time"

	"github.com/jinzhu/copier"
	"github.com/pkg/errors"
	"github.com/problame/go-netssh"

	"github.com/zrepl/zrepl/internal/config"
	"github.com/zrepl/zrepl/internal/transport"
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

func (c *SSHStdinserverConnecter) Connect(dialCtx context.Context) (transport.Wire, error) {

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
	return nconn, nil
}
