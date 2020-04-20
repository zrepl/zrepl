package tcp

import (
	"context"
	"net"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/util/tcpsock"
)

func TCPListenerFactoryFromConfig(c *config.Global, in *config.TCPServe) (transport.AuthenticatedListenerFactory, error) {
	clientMap, err := ipMapFromConfig(in.Clients)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse client IP map")
	}
	lf := func() (transport.AuthenticatedListener, error) {
		l, err := tcpsock.Listen(in.Listen, in.ListenFreeBind)
		if err != nil {
			return nil, err
		}
		return &TCPAuthListener{l, clientMap}, nil
	}
	return lf, nil
}

type TCPAuthListener struct {
	*net.TCPListener
	clientMap *ipMap
}

func (f *TCPAuthListener) Accept(ctx context.Context) (*transport.AuthConn, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		cancel()
	}()
	nc, err := f.TCPListener.AcceptTCP()
	if err != nil {
		return nil, err
	}
	clientAddr := &net.IPAddr{
		IP:   nc.RemoteAddr().(*net.TCPAddr).IP,
		Zone: nc.RemoteAddr().(*net.TCPAddr).Zone,
	}
	clientIdent, err := f.clientMap.Get(clientAddr)
	if err != nil {
		transport.GetLogger(ctx).WithField("ipaddr", clientAddr).Error("client IP not in client map")
		nc.Close()
		return nil, err
	}
	return transport.NewAuthConn(nc, clientIdent), nil
}
