package versionhandshake

import (
	"context"
	"net"
	"time"
	"github.com/zrepl/zrepl/transport"
)

type HandshakeConnecter struct {
	connecter transport.Connecter
	timeout time.Duration
}

func (c HandshakeConnecter) Connect(ctx context.Context) (transport.Wire, error) {
	conn, err := c.connecter.Connect(ctx)
	if err != nil {
		return nil, err
	}
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(c.timeout)
	}
	if err := DoHandshakeCurrentVersion(conn, dl); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func Connecter(connecter transport.Connecter, timeout time.Duration) HandshakeConnecter {
	return HandshakeConnecter{
		connecter: connecter,
		timeout: timeout,
	}
}

// wrapper type that performs a a protocol version handshake before returning the connection
type HandshakeListener struct {
	l transport.AuthenticatedListener
	timeout time.Duration
}

func (l HandshakeListener) Addr() (net.Addr) { return l.l.Addr() }

func (l HandshakeListener) Close() error { return l.l.Close() }

func (l HandshakeListener) Accept(ctx context.Context) (*transport.AuthConn, error) {
	conn, err := l.l.Accept(ctx)
	if err != nil {
		return nil, err
	}
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(l.timeout) // shadowing
	}
	if err := DoHandshakeCurrentVersion(conn, dl); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func Listener(l transport.AuthenticatedListener, timeout time.Duration) transport.AuthenticatedListener {
	return HandshakeListener{l, timeout}
}
