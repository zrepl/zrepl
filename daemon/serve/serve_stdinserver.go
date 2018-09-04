package serve

import (
	"github.com/problame/go-netssh"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/nethelpers"
	"io"
	"net"
	"path"
	"time"
	"context"
	"github.com/pkg/errors"
	"sync/atomic"
	"fmt"
	"os"
)

type StdinserverListenerFactory struct {
	ClientIdentities []string
	Sockdir string
}

func MultiStdinserverListenerFactoryFromConfig(g *config.Global, in *config.StdinserverServer) (f *multiStdinserverListenerFactory, err error) {

	for _, ci := range in.ClientIdentities {
		if err := ValidateClientIdentity(ci); err != nil {
			return nil, errors.Wrapf(err, "invalid client identity %q", ci)
		}
	}

	f = &multiStdinserverListenerFactory{
		ClientIdentities: in.ClientIdentities,
		Sockdir: g.Serve.StdinServer.SockDir,
	}

	return
}

type multiStdinserverListenerFactory struct {
	ClientIdentities []string
	Sockdir string
}

func (f *multiStdinserverListenerFactory) Listen() (AuthenticatedListener, error) {
	return multiStdinserverListenerFromClientIdentities(f.Sockdir, f.ClientIdentities)
}

type multiStdinserverAcceptRes struct {
	conn AuthenticatedConn
	err error
}

type MultiStdinserverListener struct {
	listeners []*stdinserverListener
	accepts chan multiStdinserverAcceptRes
	closed int32
}

// client identities must be validated
func multiStdinserverListenerFromClientIdentities(sockdir string, cis []string) (*MultiStdinserverListener, error) {
	listeners := make([]*stdinserverListener, 0, len(cis))
	var err error
	for _, ci := range cis {
		sockpath := path.Join(sockdir, ci)
		l  := &stdinserverListener{clientIdentity: ci}
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

func (m *MultiStdinserverListener) Accept(ctx context.Context) (AuthenticatedConn, error){

	if m.accepts == nil {
		m.accepts = make(chan multiStdinserverAcceptRes, len(m.listeners))
		for i := range m.listeners {
			go func(i int) {
				for atomic.LoadInt32(&m.closed) == 0 {
					fmt.Fprintf(os.Stderr, "accepting\n")
					conn, err := m.listeners[i].Accept(context.TODO())
					fmt.Fprintf(os.Stderr, "incoming\n")
					m.accepts <- multiStdinserverAcceptRes{conn, err}
				}
			}(i)
		}
	}

	res := <- m.accepts
	return res.conn, res.err

}

func (m *MultiStdinserverListener) Addr() (net.Addr) {
	return netsshAddr{}
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

// a single stdinserverListener (part of multiStinserverListener)
type stdinserverListener struct {
	l *netssh.Listener
	clientIdentity string
}

func (l stdinserverListener) Addr() net.Addr {
	return netsshAddr{}
}

func (l stdinserverListener) Accept(ctx context.Context) (AuthenticatedConn, error) {
	c, err := l.l.Accept()
	if err != nil {
		return nil, err
	}
	return netsshConnToNetConnAdatper{c, l.clientIdentity}, nil
}

func (l stdinserverListener) Close() (err error) {
	return l.l.Close()
}

type netsshAddr struct{}

func (netsshAddr) Network() string { return "netssh" }
func (netsshAddr) String() string  { return "???" }

type netsshConnToNetConnAdatper struct {
	io.ReadWriteCloser // works for both netssh.SSHConn and netssh.ServeConn
	clientIdentity string
}

func (a netsshConnToNetConnAdatper) ClientIdentity() string { return a.clientIdentity }

func (netsshConnToNetConnAdatper) LocalAddr() net.Addr { return netsshAddr{} }

func (netsshConnToNetConnAdatper) RemoteAddr() net.Addr { return netsshAddr{} }

// FIXME log warning once!
func (netsshConnToNetConnAdatper) SetDeadline(t time.Time) error { return nil }

func (netsshConnToNetConnAdatper) SetReadDeadline(t time.Time) error { return nil }

func (netsshConnToNetConnAdatper) SetWriteDeadline(t time.Time) error { return nil }
