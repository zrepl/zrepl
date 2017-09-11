package cmd

import (
	"fmt"
	"io"

	"github.com/jinzhu/copier"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/sshbytestream"
)

type SSHStdinserverConnecter struct {
	Host                 string
	User                 string
	Port                 uint16
	IdentityFile         string   `mapstructure:"identity_file"`
	TransportOpenCommand []string `mapstructure:"transport_open_command"`
	SSHCommand           string   `mapstructure:"ssh_command"`
	Options              []string
}

func parseSSHStdinserverConnecter(i map[string]interface{}) (c *SSHStdinserverConnecter, err error) {

	c = &SSHStdinserverConnecter{}
	if err = mapstructure.Decode(i, c); err != nil {
		err = errors.New(fmt.Sprintf("could not parse ssh transport: %s", err))
		return nil, err
	}

	// TODO assert fields are filled
	return

}

func (c *SSHStdinserverConnecter) Connect() (rwc io.ReadWriteCloser, err error) {
	var rpcTransport sshbytestream.SSHTransport
	if err = copier.Copy(&rpcTransport, c); err != nil {
		return
	}
	if rwc, err = sshbytestream.Outgoing(rpcTransport); err != nil {
		err = errors.WithStack(err)
		return
	}
	return
}
