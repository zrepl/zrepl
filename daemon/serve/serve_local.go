package serve

import (
	"context"
	"fmt"
	"github.com/zrepl/zrepl/config"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"sync"
)

var localListenerSwitchboardSingleton struct {
	s *LocalListenerSwitchboard
	once sync.Once
}

func GetLocalListenerSwitchboard() (*LocalListenerSwitchboard) {
	localListenerSwitchboardSingleton.once.Do(func() {
		localListenerSwitchboardSingleton.s = &LocalListenerSwitchboard{
			connects: make(chan connectRequest),
		}
	})
	return localListenerSwitchboardSingleton.s
}

type connectRequest struct {
	clientIdentity string
	callback chan connectResult
}

type connectResult struct {
	conn net.Conn
	err error
}

type LocalListenerSwitchboard struct {
	connects chan connectRequest
}

func (l *LocalListenerSwitchboard) DialContext(dialCtx context.Context, clientIdentity string) (conn net.Conn, err error) {

	// place request
	req := connectRequest{
		clientIdentity: clientIdentity,
		callback: make(chan connectResult),
	}
	select {
	case l.connects <- req:
	case <-dialCtx.Done():
		return nil, dialCtx.Err()
	}

	// wait for listener response
	select {
	case connRes := <- req.callback:
		conn, err = connRes.conn, connRes.err
	case <-dialCtx.Done():
		close(req.callback) // sending to the channel afterwards will panic, the listener has to catch this
		conn, err = nil, dialCtx.Err()
	}

	return conn, err
}

type localAddr struct {
	S string
}

func (localAddr) Network() string { return "local" }

func (a localAddr) String() string { return a.S }

func (l *LocalListenerSwitchboard) Addr() (net.Addr) { return localAddr{"<listening>"} }

type localConn struct {
	net.Conn
	clientIdentity string
}

func (l localConn) ClientIdentity() string { return l.clientIdentity }

func (l *LocalListenerSwitchboard) Accept(ctx context.Context) (AuthenticatedConn, error) {
	respondToRequest := func(req connectRequest, res connectResult) (err error) {
		getLogger(ctx).
			WithField("res.conn", res.conn).WithField("res.err", res.err).
			Debug("responding to client request")
		defer func() {
			errv := recover()
			getLogger(ctx).WithField("recover_err", errv).
				Debug("panic on send to client callback, likely a legitimate client-side timeout")
		}()
		select {
		case req.callback <- res:
			err = nil
		default:
			err = fmt.Errorf("client-provided callback did block on send")
		}
		close(req.callback)
		return err
	}

	getLogger(ctx).Debug("waiting for local client connect requests")
	var req connectRequest
	select {
	case req = <-l.connects:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	getLogger(ctx).WithField("client_identity", req.clientIdentity).Debug("got connect request")
	if req.clientIdentity == "" {
		res := connectResult{nil, fmt.Errorf("client identity must not be empty")}
		if err := respondToRequest(req, res); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("client connected with empty client identity")
	}

	getLogger(ctx).Debug("creating socketpair")
	left, right, err := makeSocketpairConn()
	if err != nil {
		res := connectResult{nil, fmt.Errorf("server error: %s", err)}
		if respErr := respondToRequest(req, res); respErr != nil {
			// returning the socketpair error properly is more important than the error sent to the client
			getLogger(ctx).WithError(respErr).Error("error responding to client")
		}
		return nil, err
	}

	getLogger(ctx).Debug("responding with left side of socketpair")
	res := connectResult{left, nil}
	if err := respondToRequest(req, res); err != nil {
		getLogger(ctx).WithError(err).Error("error responding to client")
		if err := left.Close(); err != nil {
			getLogger(ctx).WithError(err).Error("cannot close left side of socketpair")
		}
		if err := right.Close(); err != nil {
			getLogger(ctx).WithError(err).Error("cannot close right side of socketpair")
		}
		return nil, err
	}

	return localConn{right, req.clientIdentity}, nil
}

type fileConn struct {
	net.Conn // net.FileConn
	f *os.File
}

func (c fileConn) Close() error {
	if err := c.Conn.Close(); err != nil {
		return err
	}
	if err := c.f.Close(); err != nil {
		return err
	}
	return nil
}

func makeSocketpairConn() (a, b net.Conn, err error) {
	// don't use net.Pipe, as it doesn't implement things like lingering, which our code relies on
	sockpair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, nil, err
	}
	toConn := func(fd int) (net.Conn, error) {
		f := os.NewFile(uintptr(fd), "fileconn")
		if f == nil {
			panic(fd)
		}
		c, err := net.FileConn(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return fileConn{Conn: c, f: f}, nil
	}
	if a, err = toConn(sockpair[0]); err != nil { // shadowing
		return nil, nil, err
	}
	if b, err = toConn(sockpair[1]); err != nil { // shadowing
		a.Close()
		return nil, nil, err
	}
	return a, b, nil
}

func (l *LocalListenerSwitchboard) Close() error {
	// FIXME: make sure concurrent Accepts return with error, and further Accepts return that error, too
	// Example impl: for each accept, do context.WithCancel, and store the cancel in a list
	// When closing, set a member variable to state=closed, make sure accept will exit early
	// and then call all cancels in the list
	// The code path from Accept entry over check if state=closed to list entry must be protected by a mutex.
	return nil
}

type LocalListenerFactory struct {
	clients []string
}

func LocalListenerFactoryFromConfig(g *config.Global, in *config.LocalServe) (f *LocalListenerFactory, err error) {
	return &LocalListenerFactory{}, nil
}


func (*LocalListenerFactory) Listen() (AuthenticatedListener, error) {
	return GetLocalListenerSwitchboard(), nil
}

