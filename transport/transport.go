// Package transport defines a common interface for
// network connections that have an associated client identity.
package transport

import (
	"context"
	"errors"
	"net"
	"syscall"

	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/rpc/dataconn/timeoutconn"
	"github.com/zrepl/zrepl/zfs"
)

type AuthConn struct {
	Wire
	clientIdentity string
}

var _ timeoutconn.SyscallConner = AuthConn{}

func (a AuthConn) SyscallConn() (rawConn syscall.RawConn, err error) {
	scc, ok := a.Wire.(timeoutconn.SyscallConner)
	if !ok {
		return nil, timeoutconn.SyscallConnNotSupported
	}
	return scc.SyscallConn()
}

func NewAuthConn(conn Wire, clientIdentity string) *AuthConn {
	return &AuthConn{conn, clientIdentity}
}

func (c *AuthConn) ClientIdentity() string {
	if err := ValidateClientIdentity(c.clientIdentity); err != nil {
		panic(err)
	}
	return c.clientIdentity
}

// like net.Listener, but with an AuthenticatedConn instead of net.Conn
type AuthenticatedListener interface {
	Addr() net.Addr
	Accept(ctx context.Context) (*AuthConn, error)
	Close() error
}

type AuthenticatedListenerFactory func() (AuthenticatedListener, error)

type Wire = timeoutconn.Wire

type Connecter interface {
	Connect(ctx context.Context) (Wire, error)
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

type contextKey int

const contextKeyLog contextKey = 0

type Logger = logger.Logger

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLog, log)
}

func GetLogger(ctx context.Context) Logger {
	if log, ok := ctx.Value(contextKeyLog).(Logger); ok {
		return log
	}
	return logger.NewNullLogger()
}
