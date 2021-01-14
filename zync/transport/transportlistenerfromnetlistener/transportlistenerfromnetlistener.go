package transportlistenerfromnetlistener

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/zrepl/zrepl/transport"
)

type wrapFixed struct {
	id string
	l  net.Listener
}

var _ transport.AuthenticatedListener = (*wrapFixed)(nil)

func WrapFixed(l net.Listener, identity string) transport.AuthenticatedListener {
	return &wrapFixed{identity, l}
}

func (w *wrapFixed) Addr() net.Addr {
	return w.l.Addr()
}

type fakeWire struct {
	net.Conn
}

func (w *fakeWire) CloseWrite() error {
	time.Sleep(1*time.Second) // HACKY
	return fmt.Errorf("fakeWire does not support CloseWrite")
}

func (w *wrapFixed) Accept(ctx context.Context) (*transport.AuthConn, error) {
	nc, err := w.l.Accept()
	if err != nil {
		return nil, err
	}

	return transport.NewAuthConn(&fakeWire{nc}, w.id), nil
}

func (w *wrapFixed) Close() error {
	return w.l.Close()
}
