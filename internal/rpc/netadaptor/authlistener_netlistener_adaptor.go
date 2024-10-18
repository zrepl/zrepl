// Package netadaptor implements an adaptor from
// transport.AuthenticatedListener to net.Listener.
package netadaptor

import (
	"context"
	"fmt"
	"net"

	"github.com/zrepl/zrepl/internal/logger"
	"github.com/zrepl/zrepl/internal/transport"
)

type Logger = logger.Logger

type acceptRes struct {
	conn *transport.AuthConn
	err  error
}
type acceptReq struct {
	callback chan acceptRes
}

type Listener struct {
	al      transport.AuthenticatedListener
	logger  Logger
	accepts chan acceptReq
	stop    chan struct{}
}

// Consume the given authListener and wrap it as a *Listener, which implements net.Listener.
// The returned net.Listener must be closed.
// The wrapped authListener is closed when the returned net.Listener is closed.
func New(authListener transport.AuthenticatedListener, l Logger) *Listener {
	if l == nil {
		l = logger.NewNullLogger()
	}
	a := &Listener{
		al:      authListener,
		logger:  l,
		accepts: make(chan acceptReq),
		stop:    make(chan struct{}),
	}
	go a.handleAccept()
	return a
}

// The returned net.Conn is guaranteed to be *transport.AuthConn, i.e., the type of connection
// returned by the wrapped transport.AuthenticatedListener.
func (a Listener) Accept() (net.Conn, error) {
	req := acceptReq{make(chan acceptRes, 1)}

	select {
	case a.accepts <- req:
	case <-a.stop:
		return nil, fmt.Errorf("already closed") // TODO net.Error
	}

	res, ok := <-req.callback
	if !ok {
		return nil, fmt.Errorf("already closed") // TODO net.Error
	}
	return res.conn, res.err
}

func (a Listener) handleAccept() {
	for {
		select {
		case <-a.stop:
			a.logger.Debug("handleAccept stop accepting")
			return
		case req := <-a.accepts:
			a.logger.Debug("accept authListener")
			authConn, err := a.al.Accept(context.Background())
			req.callback <- acceptRes{authConn, err}
		}
	}
}

func (a Listener) Addr() net.Addr {
	return a.al.Addr()
}

func (a Listener) Close() error {
	close(a.stop)
	return a.al.Close()
}
