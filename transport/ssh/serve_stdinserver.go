package ssh

import (
	"context"
	"fmt"
	"net"
	"path"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/problame/go-netssh"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/nethelpers"
	"github.com/zrepl/zrepl/transport"
)

func MultiStdinserverListenerFactoryFromConfig(g *config.Global, in *config.StdinserverServer) (transport.AuthenticatedListenerFactory, error) {

	for _, ci := range in.ClientIdentities {
		if err := transport.ValidateClientIdentity(ci); err != nil {
			return nil, errors.Wrapf(err, "invalid client identity %q", ci)
		}
	}

	clientIdentities := in.ClientIdentities
	sockdir := g.Serve.StdinServer.SockDir

	lf := func() (transport.AuthenticatedListener, error) {
		return multiStdinserverListenerFromClientIdentities(sockdir, clientIdentities)
	}

	return lf, nil
}

type multiStdinserverAcceptRes struct {
	conn *transport.AuthConn
	err  error
}

type MultiStdinserverListener struct {
	listeners []*stdinserverListener
	accepts   chan multiStdinserverAcceptRes
	closed    int32
}

// client identities must be validated
func multiStdinserverListenerFromClientIdentities(sockdir string, cis []string) (*MultiStdinserverListener, error) {
	listeners := make([]*stdinserverListener, 0, len(cis))
	var err error
	for _, ci := range cis {
		sockpath := path.Join(sockdir, ci)
		l := &stdinserverListener{clientIdentity: ci}
		if err = nethelpers.PreparePrivateSockpath(sockpath); err != nil {
			break
		}
		if l.l, err = netssh.Listen(sockpath); err != nil {
			break
		}
		listeners = append(listeners, l)
	}
	if err != nil {
		for _, l := range listeners {
			l.Close() // FIXME error reporting?
		}
		return nil, err
	}
	return &MultiStdinserverListener{listeners: listeners}, nil
}

func (m *MultiStdinserverListener) Accept(ctx context.Context) (*transport.AuthConn, error) {

	if m.accepts == nil {
		m.accepts = make(chan multiStdinserverAcceptRes, len(m.listeners))
		for i := range m.listeners {
			go func(i int) {
				for atomic.LoadInt32(&m.closed) == 0 {
					conn, err := m.listeners[i].Accept(context.TODO())
					m.accepts <- multiStdinserverAcceptRes{conn, err}
				}
			}(i)
		}
	}

	res := <-m.accepts
	return res.conn, res.err

}

type multiListenerAddr struct {
	clients []string
}

func (multiListenerAddr) Network() string { return "netssh" }

func (l multiListenerAddr) String() string {
	return fmt.Sprintf("netssh:clients=%v", l.clients)
}

func (m *MultiStdinserverListener) Addr() net.Addr {
	cis := make([]string, len(m.listeners))
	for i := range cis {
		cis[i] = m.listeners[i].clientIdentity
	}
	return multiListenerAddr{cis}
}

func (m *MultiStdinserverListener) Close() error {
	atomic.StoreInt32(&m.closed, 1)
	var oneErr error
	for _, l := range m.listeners {
		if err := l.Close(); err != nil && oneErr == nil {
			oneErr = err
		}
	}
	return oneErr
}

// a single stdinserverListener (part of multiStdinserverListener)
type stdinserverListener struct {
	l              *netssh.Listener
	clientIdentity string
}

type listenerAddr struct {
	clientIdentity string
}

func (listenerAddr) Network() string { return "netssh" }

func (a listenerAddr) String() string {
	return fmt.Sprintf("netssh:client=%q", a.clientIdentity)
}

func (l stdinserverListener) Addr() net.Addr {
	return listenerAddr{l.clientIdentity}
}

func (l stdinserverListener) Accept(ctx context.Context) (*transport.AuthConn, error) {
	c, err := l.l.Accept()
	if err != nil {
		return nil, err
	}
	return transport.NewAuthConn(c, l.clientIdentity), nil
}

func (l stdinserverListener) Close() (err error) {
	return l.l.Close()
}
