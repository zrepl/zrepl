package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/logger"
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

type streamrpcLogAdaptor = twoClassLogAdaptor
type replicationLogAdaptor = twoClassLogAdaptor

type twoClassLogAdaptor struct {
	logger.Logger
}

var _ streamrpc.Logger = twoClassLogAdaptor{}

func (a twoClassLogAdaptor) Errorf(fmtStr string, args ...interface{}) {
	const errorSuffix = ": %s"
	if len(args) == 1 {
		if err, ok := args[0].(error); ok && strings.HasSuffix(fmtStr, errorSuffix) {
			msg := strings.TrimSuffix(fmtStr, errorSuffix)
			a.WithError(err).Error(msg)
			return
		}
	}
	a.Logger.Error(fmt.Sprintf(fmtStr, args...))
}

func (a twoClassLogAdaptor) Infof(fmtStr string, args ...interface{}) {
	a.Logger.Info(fmt.Sprintf(fmtStr, args...))
}
