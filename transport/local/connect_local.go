package local

import (
	"context"
	"fmt"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/transport"
)

type LocalConnecter struct {
	listenerName   string
	clientIdentity string
}

func LocalConnecterFromConfig(in *config.LocalConnect) (*LocalConnecter, error) {
	if in.ClientIdentity == "" {
		return nil, fmt.Errorf("ClientIdentity must not be empty")
	}
	if in.ListenerName == "" {
		return nil, fmt.Errorf("ListenerName must not be empty")
	}
	return &LocalConnecter{listenerName: in.ListenerName, clientIdentity: in.ClientIdentity}, nil
}

func (c *LocalConnecter) Connect(dialCtx context.Context) (transport.Wire, error) {
	l := GetLocalListener(c.listenerName)
	return l.Connect(dialCtx, c.clientIdentity)
}
