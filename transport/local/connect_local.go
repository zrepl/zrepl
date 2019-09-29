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
	dialTimeout    time.Duration
}

func LocalConnecterFromConfig(in *config.LocalConnect) (*LocalConnecter, error) {
	if in.ClientIdentity == "" {
		return nil, fmt.Errorf("ClientIdentity must not be empty")
	}
	if in.ListenerName == "" {
		return nil, fmt.Errorf("ListenerName must not be empty")
	}
	if in.DialTimeout < 0 {
		return nil, fmt.Errorf("DialTimeout must be zero or positive")
	}
	cn := &LocalConnecter{
		listenerName:   in.ListenerName,
		clientIdentity: in.ClientIdentity,
		dialTimeout:    in.DialTimeout,
	}
	return cn, nil
}

func (c *LocalConnecter) Connect(dialCtx context.Context) (transport.Wire, error) {
	l := GetLocalListener(c.listenerName)
	if c.dialTimeout > 0 {
		ctx, cancel := context.WithTimeout(dialCtx, c.dialTimeout)
		defer cancel()
		dialCtx = ctx // shadow
	}
	w, err := l.Connect(dialCtx, c.clientIdentity)
	if err == context.DeadlineExceeded {
		return nil, fmt.Errorf("local listener %q not reachable", c.listenerName)
	}
	return w, err
}
