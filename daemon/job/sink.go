package job

import (
	"context"
	"github.com/pkg/errors"
	"github.com/problame/go-streamrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/daemon/serve"
	"github.com/zrepl/zrepl/endpoint"
	"path"
	"github.com/zrepl/zrepl/zfs"
)

type Sink struct {
	name     string
	l        serve.ListenerFactory
	rpcConf  *streamrpc.ConnConfig
	rootDataset *zfs.DatasetPath
}

func SinkFromConfig(g *config.Global, in *config.SinkJob) (s *Sink, err error) {

	s = &Sink{name: in.Name}
	if s.l, s.rpcConf, err = serve.FromConfig(g, in.Serve); err != nil {
		return nil, errors.Wrap(err, "cannot build server")
	}

	s.rootDataset, err = zfs.NewDatasetPath(in.RootDataset)
	if err != nil {
		return nil, errors.New("root dataset is not a valid zfs filesystem path")
	}
	if s.rootDataset.Length() <= 0 {
		return nil, errors.New("root dataset must not be empty") // duplicates error check of receiver
	}


	return s, nil
}

func (j *Sink) Name() string { return j.name }

func (*Sink) Status() interface{} {
	// FIXME
	return nil
}

func (*Sink) RegisterMetrics(registerer prometheus.Registerer) {}

func (j *Sink) Run(ctx context.Context) {

	log := GetLogger(ctx)
	defer log.Info("job exiting")

	l, err := j.l.Listen()
	if err != nil {
		log.WithError(err).Error("cannot listen")
		return
	}
	defer l.Close()

	log.WithField("addr", l.Addr()).Debug("accepting connections")

	var connId int

outer:
	for {

		select {
		case res := <-accept(ctx, l):
			if res.err != nil {
				log.WithError(res.err).Info("accept error")
				continue
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

func (j *Sink) handleConnection(ctx context.Context, conn serve.AuthenticatedConn) {
	defer conn.Close()

	log := GetLogger(ctx)
	log.
		WithField("addr", conn.RemoteAddr()).
		WithField("client_identity", conn.ClientIdentity()).
		Info("handling connection")
	defer log.Info("finished handling connection")

	clientRootStr := path.Join(j.rootDataset.ToString(), conn.ClientIdentity())
	clientRoot, err := zfs.NewDatasetPath(clientRootStr)
	if err != nil {
		log.WithError(err).
			WithField("client_identity", conn.ClientIdentity()).
			Error("cannot build client filesystem map (client identity must be a valid ZFS FS name")
	}
	log.WithField("client_root", clientRoot).Debug("client root")

	ctx = logging.WithSubsystemLoggers(ctx, log)

	local, err := endpoint.NewReceiver(clientRoot)
	if err != nil {
		log.WithError(err).Error("unexpected error: cannot convert mapping to filter")
		return
	}

	handler := endpoint.NewHandler(local)
	if err := streamrpc.ServeConn(ctx, conn, j.rpcConf, handler.Handle); err != nil {
		log.WithError(err).Error("error serving client")
	}
}

type acceptResult struct {
	conn serve.AuthenticatedConn
	err  error
}

func accept(ctx context.Context, listener serve.AuthenticatedListener) <-chan acceptResult {
	c := make(chan acceptResult, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		c <- acceptResult{conn, err}
	}()
	return c
}
