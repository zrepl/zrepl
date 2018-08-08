package cmd

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/util"
)

type logNetConnConnecter struct {
	streamrpc.Connecter
	ReadDump, WriteDump string
}

var _ streamrpc.Connecter = logNetConnConnecter{}

func (l logNetConnConnecter) Connect(ctx context.Context) (net.Conn, error) {
	conn, err := l.Connecter.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return util.NewNetConnLogger(conn, l.ReadDump, l.WriteDump)
}

type logListenerFactory struct {
    ListenerFactory
    ReadDump, WriteDump string
}

var _ ListenerFactory = logListenerFactory{}

type logListener struct {
    net.Listener
    ReadDump, WriteDump string
}

var _ net.Listener = logListener{}

func (m logListenerFactory) Listen() (net.Listener, error) {
    l, err := m.ListenerFactory.Listen()
    if err != nil {
        return nil, err
    }
    return logListener{l, m.ReadDump, m.WriteDump}, nil
}

func (l logListener) Accept() (net.Conn, error) {
    conn, err := l.Listener.Accept()
    if err != nil {
        return nil, err
    }
    return util.NewNetConnLogger(conn, l.ReadDump, l.WriteDump)
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
