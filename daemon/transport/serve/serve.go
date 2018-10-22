package serve

import (
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/transport"
	"net"
	"github.com/zrepl/zrepl/daemon/streamrpcconfig"
	"github.com/problame/go-streamrpc"
	"context"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/zfs"
	"time"
)

type contextKey int

const contextKeyLog contextKey = 0

type Logger = logger.Logger

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLog, log)
}

func getLogger(ctx context.Context) Logger {
	if log, ok := ctx.Value(contextKeyLog).(Logger); ok {
		return log
	}
	return logger.NewNullLogger()
}

type AuthenticatedConn interface {
	net.Conn
	// ClientIdentity must be a string that satisfies ValidateClientIdentity
	ClientIdentity() string
}

// A client identity must be a single component in a ZFS filesystem path
func ValidateClientIdentity(in string) (err error) {
	path, err := zfs.NewDatasetPath(in)
	if err != nil {
		return err
	}
	if path.Length() != 1 {
		return errors.New("client identity must be a single path comonent (not empty, no '/')")
	}
	return nil
}

type authConn struct {
	net.Conn
	clientIdentity string
}

var _ AuthenticatedConn = authConn{}

func (c authConn) ClientIdentity() string {
	if err := ValidateClientIdentity(c.clientIdentity); err != nil {
		panic(err)
	}
	return c.clientIdentity
}

// like net.Listener, but with an AuthenticatedConn instead of net.Conn
type AuthenticatedListener interface {
	Addr() (net.Addr)
	Accept(ctx context.Context) (AuthenticatedConn, error)
	Close() error
}

type ListenerFactory interface {
	Listen() (AuthenticatedListener, error)
}

type HandshakeListenerFactory struct {
	lf ListenerFactory
}

func (lf HandshakeListenerFactory) Listen() (AuthenticatedListener, error) {
	l, err := lf.lf.Listen()
	if err != nil {
		return nil, err
	}
	return HandshakeListener{l}, nil
}

type HandshakeListener struct {
	l AuthenticatedListener
}

func (l HandshakeListener) Addr() (net.Addr) { return l.l.Addr() }

func (l HandshakeListener) Close() error { return l.l.Close() }

func (l HandshakeListener) Accept(ctx context.Context) (AuthenticatedConn, error) {
	conn, err := l.l.Accept(ctx)
	if err != nil {
		return nil, err
	}
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(10*time.Second) // FIXME constant
	}
	if err := transport.DoHandshakeCurrentVersion(conn, dl); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
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
		lf, lfError = MultiStdinserverListenerFactoryFromConfig(g, v)
		conf, rpcErr = streamrpcconfig.FromDaemonConfig(g, v.RPC)
	case *config.LocalServe:
		lf, lfError = LocalListenerFactoryFromConfig(g, v)
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

	lf = HandshakeListenerFactory{lf}

	return lf, conf, nil

}


