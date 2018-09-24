package connecter

import (
	"context"
	"fmt"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/serve"
	"net"
)

type LocalConnecter struct {
	clientIdentity string
}

func LocalConnecterFromConfig(in *config.LocalConnect) (*LocalConnecter, error) {
	if in.ClientIdentity == "" {
		return nil, fmt.Errorf("ClientIdentity must not be empty")
	}
	return &LocalConnecter{in.ClientIdentity}, nil
}

func (c *LocalConnecter) Connect(dialCtx context.Context) (conn net.Conn, err error) {
	switchboard := serve.GetLocalListenerSwitchboard()
	return switchboard.DialContext(dialCtx, c.clientIdentity)
}

