package serve

import (
	"github.com/problame/go-netssh"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/nethelpers"
	"io"
	"net"
	"path"
	"time"
)

type StdinserverListenerFactory struct {
	ClientIdentity string
	sockpath       string
}

func StdinserverListenerFactoryFromConfig(g *config.Global, in *config.StdinserverServer) (f *StdinserverListenerFactory, err error) {

	f = &StdinserverListenerFactory{
		ClientIdentity: in.ClientIdentity,
	}

	f.sockpath = path.Join(g.Serve.StdinServer.SockDir, f.ClientIdentity)

	return
}

func (f *StdinserverListenerFactory) Listen() (net.Listener, error) {

	if err := nethelpers.PreparePrivateSockpath(f.sockpath); err != nil {
		return nil, err
	}

	l, err := netssh.Listen(f.sockpath)
	if err != nil {
		return nil, err
	}
	return StdinserverListener{l}, nil
}

type StdinserverListener struct {
	l *netssh.Listener
}

func (l StdinserverListener) Addr() net.Addr {
	return netsshAddr{}
}

func (l StdinserverListener) Accept() (net.Conn, error) {
	c, err := l.l.Accept()
	if err != nil {
		return nil, err
	}
	return netsshConnToNetConnAdatper{c}, nil
}

func (l StdinserverListener) Close() (err error) {
	return l.l.Close()
}

type netsshAddr struct{}

func (netsshAddr) Network() string { return "netssh" }
func (netsshAddr) String() string  { return "???" }

type netsshConnToNetConnAdatper struct {
	io.ReadWriteCloser // works for both netssh.SSHConn and netssh.ServeConn
}

func (netsshConnToNetConnAdatper) LocalAddr() net.Addr { return netsshAddr{} }

func (netsshConnToNetConnAdatper) RemoteAddr() net.Addr { return netsshAddr{} }

func (netsshConnToNetConnAdatper) SetDeadline(t time.Time) error { return nil }

func (netsshConnToNetConnAdatper) SetReadDeadline(t time.Time) error { return nil }

func (netsshConnToNetConnAdatper) SetWriteDeadline(t time.Time) error { return nil }
