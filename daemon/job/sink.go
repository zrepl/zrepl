package job

import (
	"context"
	"github.com/pkg/errors"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/daemon/serve"
	"github.com/zrepl/zrepl/endpoint"
	"net"
)

type Sink struct {
	name     string
	l        serve.ListenerFactory
	fsmap    endpoint.FSMap
	fsmapInv endpoint.FSFilter
}

func SinkFromConfig(g config.Global, in *config.SinkJob) (s *Sink, err error) {

	// FIXME multi client support

	s = &Sink{name: in.Name}
	if s.l, err = serve.FromConfig(g, in.Replication.Serve); err != nil {
		return nil, errors.Wrap(err, "cannot build server")
	}

	fsmap := filters.NewDatasetMapFilter(1, false) // FIXME multi-client support
	if err := fsmap.Add("<", in.Replication.RootDataset); err != nil {
		return nil, errors.Wrap(err, "unexpected error: cannot build filesystem mapping")
	}
	s.fsmap = fsmap

	return s, nil
}

func (j *Sink) Name() string { return j.name }

func (*Sink) Status() interface{} {
	// FIXME
	return nil
}

func (j *Sink) Run(ctx context.Context) {

	log := GetLogger(ctx)
	defer log.Info("job exiting")

	l, err := j.l.Listen()
	if err != nil {
		log.WithError(err).Error("cannot listen")
		return
	}

	log.WithField("addr", l.Addr()).Debug("accepting connections")

	var connId int

outer:
	for {

		select {
		case res := <-accept(l):
			if res.err != nil {
				log.WithError(err).Info("accept error")
				break outer
			}
			connId++
			connLog := log.
				WithField("connID", connId)
			j.handleConnection(WithLogger(ctx, connLog), res.conn)

		case <-ctx.Done():
			break outer
		}

	}

}

func (j *Sink) handleConnection(ctx context.Context, conn net.Conn) {
	log := GetLogger(ctx)
	log.WithField("addr", conn.RemoteAddr()).Info("handling connection")
	defer log.Info("finished handling connection")

	ctx = logging.WithSubsystemLoggers(ctx, log)

	local, err := endpoint.NewReceiver(j.fsmap, filters.NewAnyFSVFilter())
	if err != nil {
		log.WithError(err).Error("unexpected error: cannot convert mapping to filter")
		return
	}

	handler := endpoint.NewHandler(local)
	if err := streamrpc.ServeConn(ctx, conn, STREAMRPC_CONFIG, handler.Handle); err != nil {
		log.WithError(err).Error("error serving client")
	}
}

type acceptResult struct {
	conn net.Conn
	err  error
}

func accept(listener net.Listener) <-chan acceptResult {
	c := make(chan acceptResult, 1)
	go func() {
		conn, err := listener.Accept()
		c <- acceptResult{conn, err}
	}()
	return c
}
