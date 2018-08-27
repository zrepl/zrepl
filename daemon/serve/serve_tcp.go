package serve

import (
	"github.com/zrepl/zrepl/config"
	"net"
)

type TCPListenerFactory struct {
	Address string
}

func TCPListenerFactoryFromConfig(c config.Global, in *config.TCPServe) (*TCPListenerFactory, error) {
	lf := &TCPListenerFactory{
		Address: in.Listen,
	}
	return lf, nil
}

func (f *TCPListenerFactory) Listen() (net.Listener, error) {
	return net.Listen("tcp", f.Address)
}
