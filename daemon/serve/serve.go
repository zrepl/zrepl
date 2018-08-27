package serve

import (
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/config"
	"net"
)

type ListenerFactory interface {
	Listen() (net.Listener, error)
}

func FromConfig(g config.Global, in config.ServeEnum) (ListenerFactory, error) {

	switch v := in.Ret.(type) {
	case *config.TCPServe:
		return TCPListenerFactoryFromConfig(g, v)
	case *config.TLSServe:
		return TLSListenerFactoryFromConfig(g, v)
	case *config.StdinserverServer:
		return StdinserverListenerFactoryFromConfig(g, v)
	default:
		return nil, errors.Errorf("internal error: unknown serve type %T", v)
	}

}
