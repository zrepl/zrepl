package serve

import (
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/config"
	"net"
	"github.com/zrepl/zrepl/daemon/streamrpcconfig"
	"github.com/problame/go-streamrpc"
)

type ListenerFactory interface {
	Listen() (net.Listener, error)
}

func FromConfig(g *config.Global, in config.ServeEnum) (lf ListenerFactory, conf *streamrpc.ConnConfig, _ error) {

	var (
		lfError, rpcErr error
	)
	switch v := in.Ret.(type) {
	case *config.TCPServe:
		lf, lfError = TCPListenerFactoryFromConfig(g, v)
		conf, rpcErr = streamrpcconfig.FromDaemonConfig(g, v.RPC)
	case *config.TLSServe:
		lf, lfError = TLSListenerFactoryFromConfig(g, v)
		conf, rpcErr = streamrpcconfig.FromDaemonConfig(g, v.RPC)
	case *config.StdinserverServer:
		lf, lfError = StdinserverListenerFactoryFromConfig(g, v)
		conf, rpcErr = streamrpcconfig.FromDaemonConfig(g, v.RPC)
	default:
		return nil, nil, errors.Errorf("internal error: unknown serve type %T", v)
	}

	if lfError != nil {
		return nil, nil, lfError
	}
	if rpcErr != nil {
		return nil, nil, rpcErr
	}

	return lf, conf, nil

}


