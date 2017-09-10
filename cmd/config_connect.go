package cmd

import (
	"fmt"
	"io"

	"github.com/jinzhu/copier"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/sshbytestream"
	"github.com/zrepl/zrepl/util"
)

type SSHStdinserverConnecter struct {
	Host                 string
	User                 string
	Port                 uint16
	IdentityFile         string   `mapstructure:"identity_file"`
	TransportOpenCommand []string `mapstructure:"transport_open_command"`
	SSHCommand           string   `mapstructure:"ssh_command"`
	Options              []string
	ConnLogReadFile      string `mapstructure:"connlog_read_file"`
	ConnLogWriteFile     string `mapstructure:"connlog_write_file"`
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

func (c *SSHStdinserverConnecter) Connect() (client rpc.RPCClient, err error) {
	var stream io.ReadWriteCloser
	var rpcTransport sshbytestream.SSHTransport
	if err = copier.Copy(&rpcTransport, c); err != nil {
		return
	}
	if stream, err = sshbytestream.Outgoing(rpcTransport); err != nil {
		return
	}
	stream, err = util.NewReadWriteCloserLogger(stream, c.ConnLogReadFile, c.ConnLogWriteFile)
	if err != nil {
		return
	}
	client = rpc.NewClient(stream)
	return client, nil

}
