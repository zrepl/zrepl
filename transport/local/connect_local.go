package local

import (
	"context"
	"fmt"
	"time"

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
	dialCtx, cancel := context.WithTimeout(dialCtx, 1*time.Second) // fail fast, config error by user is very likely
	defer cancel()
	w, err := l.Connect(dialCtx, c.clientIdentity)
	if err == context.DeadlineExceeded {
		return nil, fmt.Errorf("local listener %q not reachable", c.listenerName)
	}
	return w, err
}
