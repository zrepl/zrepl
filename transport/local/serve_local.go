package local

import (
	"context"
	"fmt"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/util/socketpair"
	"net"
	"sync"
	"github.com/zrepl/zrepl/transport"
)

var localListeners struct {
	m map[string]*LocalListener // listenerName -> listener
	init sync.Once
	mtx sync.Mutex
}

func GetLocalListener(listenerName string) (*LocalListener) {

	localListeners.init.Do(func() {
		localListeners.m = make(map[string]*LocalListener)
	})

	localListeners.mtx.Lock()
	defer localListeners.mtx.Unlock()

	l, ok := localListeners.m[listenerName]
	if !ok {
		l = newLocalListener()
		localListeners.m[listenerName] = l
	}
	return l

}

type connectRequest struct {
	clientIdentity string
	callback chan connectResult
}

type connectResult struct {
	conn transport.Wire
	err error
}

type LocalListener struct {
	connects chan connectRequest
}

func newLocalListener() *LocalListener {
	return &LocalListener{
		connects: make(chan connectRequest),
	}
}

// Connect to the LocalListener from a client with identity clientIdentity
func (l *LocalListener) Connect(dialCtx context.Context, clientIdentity string) (conn transport.Wire, err error) {

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

func (l *LocalListener) Addr() (net.Addr) { return localAddr{"<listening>"} }

func (l *LocalListener) Accept(ctx context.Context) (*transport.AuthConn, error) {
	respondToRequest := func(req connectRequest, res connectResult) (err error) {
		transport.GetLogger(ctx).
			WithField("res.conn", res.conn).WithField("res.err", res.err).
			Debug("responding to client request")
		defer func() {
			errv := recover()
			transport.GetLogger(ctx).WithField("recover_err", errv).
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

	transport.GetLogger(ctx).Debug("waiting for local client connect requests")
	var req connectRequest
	select {
	case req = <-l.connects:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	transport.GetLogger(ctx).WithField("client_identity", req.clientIdentity).Debug("got connect request")
	if req.clientIdentity == "" {
		res := connectResult{nil, fmt.Errorf("client identity must not be empty")}
		if err := respondToRequest(req, res); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("client connected with empty client identity")
	}

	transport.GetLogger(ctx).Debug("creating socketpair")
	left, right, err := socketpair.SocketPair()
	if err != nil {
		res := connectResult{nil, fmt.Errorf("server error: %s", err)}
		if respErr := respondToRequest(req, res); respErr != nil {
			// returning the socketpair error properly is more important than the error sent to the client
			transport.GetLogger(ctx).WithError(respErr).Error("error responding to client")
		}
		return nil, err
	}

	transport.GetLogger(ctx).Debug("responding with left side of socketpair")
	res := connectResult{left, nil}
	if err := respondToRequest(req, res); err != nil {
		transport.GetLogger(ctx).WithError(err).Error("error responding to client")
		if err := left.Close(); err != nil {
			transport.GetLogger(ctx).WithError(err).Error("cannot close left side of socketpair")
		}
		if err := right.Close(); err != nil {
			transport.GetLogger(ctx).WithError(err).Error("cannot close right side of socketpair")
		}
		return nil, err
	}

	return transport.NewAuthConn(right, req.clientIdentity), nil
}

func (l *LocalListener) Close() error {
	// FIXME: make sure concurrent Accepts return with error, and further Accepts return that error, too
	// Example impl: for each accept, do context.WithCancel, and store the cancel in a list
	// When closing, set a member variable to state=closed, make sure accept will exit early
	// and then call all cancels in the list
	// The code path from Accept entry over check if state=closed to list entry must be protected by a mutex.
	return nil
}

func LocalListenerFactoryFromConfig(g *config.Global, in *config.LocalServe) (transport.AuthenticatedListenerFactory,error) {
	if in.ListenerName == "" {
		return nil, fmt.Errorf("ListenerName must not be empty")
	}
	listenerName := in.ListenerName
	lf := func() (transport.AuthenticatedListener,error) {
		return GetLocalListener(listenerName), nil
	}
	return lf, nil
}
