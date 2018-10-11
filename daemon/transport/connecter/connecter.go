package connecter

import (
	"context"
	"fmt"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/streamrpcconfig"
	"github.com/zrepl/zrepl/daemon/transport"
	"net"
	"time"
)


type HandshakeConnecter struct {
	connecter streamrpc.Connecter
}

func (c HandshakeConnecter) Connect(ctx context.Context) (net.Conn, error) {
	conn, err := c.connecter.Connect(ctx)
	if err != nil {
		return nil, err
	}
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(10 * time.Second) // FIXME constant
	}
	if err := transport.DoHandshakeCurrentVersion(conn, dl); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}



func FromConfig(g *config.Global, in config.ConnectEnum) (*ClientFactory, error) {
	var (
		connecter            streamrpc.Connecter
		errConnecter, errRPC error
		connConf             *streamrpc.ConnConfig
	)
	switch v := in.Ret.(type) {
	case *config.SSHStdinserverConnect:
		connecter, errConnecter = SSHStdinserverConnecterFromConfig(v)
		connConf, errRPC = streamrpcconfig.FromDaemonConfig(g, v.RPC)
	case *config.TCPConnect:
		connecter, errConnecter = TCPConnecterFromConfig(v)
		connConf, errRPC = streamrpcconfig.FromDaemonConfig(g, v.RPC)
	case *config.TLSConnect:
		connecter, errConnecter = TLSConnecterFromConfig(v)
		connConf, errRPC = streamrpcconfig.FromDaemonConfig(g, v.RPC)
	case *config.LocalConnect:
		connecter, errConnecter = LocalConnecterFromConfig(v)
		connConf, errRPC = streamrpcconfig.FromDaemonConfig(g, v.RPC)
	default:
		panic(fmt.Sprintf("implementation error: unknown connecter type %T", v))
	}

	if errConnecter != nil {
		return nil, errConnecter
	}
	if errRPC != nil {
		return nil, errRPC
	}

	config := streamrpc.ClientConfig{ConnConfig: connConf}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	connecter = HandshakeConnecter{connecter}

	return &ClientFactory{connecter: connecter, config: &config}, nil
}

type ClientFactory struct {
	connecter streamrpc.Connecter
	config    *streamrpc.ClientConfig
}

func (f ClientFactory) NewClient() (*streamrpc.Client, error) {
	return streamrpc.NewClient(f.connecter, f.config)
}
