// Package transport defines a common interface for
// network connections that have an associated client identity.
package transport

import (
	"context"
	"net"
	"syscall"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/internal/daemon/logging"
	"github.com/zrepl/zrepl/internal/logger"
	"github.com/zrepl/zrepl/internal/rpc/dataconn/timeoutconn"
	"github.com/zrepl/zrepl/internal/zfs"
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
func ValidateClientIdentity(in string) error {
	err := zfs.ComponentNamecheck(in)
	if err != nil {
		return errors.Wrap(err, "client identity must be usable as a single dataset path component")
	}
	return nil
}

type Logger = logger.Logger

func GetLogger(ctx context.Context) Logger {
	return logging.GetLogger(ctx, logging.SubsysTransport)
}
