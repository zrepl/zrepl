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

type PassiveSide struct {
	mode passiveMode
	name     string
	l        serve.ListenerFactory
	rpcConf  *streamrpc.ConnConfig
}

type passiveMode interface {
	ConnHandleFunc(ctx context.Context, conn serve.AuthenticatedConn) streamrpc.HandlerFunc
	Type() Type
}

type modeSink struct {
	rootDataset *zfs.DatasetPath
}

func (m *modeSink) Type() Type { return TypeSink }

func (m *modeSink) ConnHandleFunc(ctx context.Context, conn serve.AuthenticatedConn) streamrpc.HandlerFunc {
	log := GetLogger(ctx)

	clientRootStr := path.Join(m.rootDataset.ToString(), conn.ClientIdentity())
	clientRoot, err := zfs.NewDatasetPath(clientRootStr)
	if err != nil {
		log.WithError(err).
			WithField("client_identity", conn.ClientIdentity()).
			Error("cannot build client filesystem map (client identity must be a valid ZFS FS name")
	}
	log.WithField("client_root", clientRoot).Debug("client root")

	local, err := endpoint.NewReceiver(clientRoot)
	if err != nil {
		log.WithError(err).Error("unexpected error: cannot convert mapping to filter")
		return nil
	}

	h := endpoint.NewHandler(local)
	return h.Handle
}

func modeSinkFromConfig(g *config.Global, in *config.SinkJob) (m *modeSink, err error) {
	m = &modeSink{}
	m.rootDataset, err = zfs.NewDatasetPath(in.RootDataset)
	if err != nil {
		return nil, errors.New("root dataset is not a valid zfs filesystem path")
	}
	if m.rootDataset.Length() <= 0 {
		return nil, errors.New("root dataset must not be empty") // duplicates error check of receiver
	}
	return m, nil
}

func passiveSideFromConfig(g *config.Global, in *config.PassiveJob, mode passiveMode) (s *PassiveSide, err error) {

	s = &PassiveSide{mode: mode, name: in.Name}
	if s.l, s.rpcConf, err = serve.FromConfig(g, in.Serve); err != nil {
		return nil, errors.Wrap(err, "cannot build server")
	}

	return s, nil
}

func (j *PassiveSide) Name() string { return j.name }

type PassiveStatus struct {}

func (s *PassiveSide) Status() *Status {
	return &Status{Type: s.mode.Type()} // FIXME PassiveStatus
}

func (*PassiveSide) RegisterMetrics(registerer prometheus.Registerer) {}

func (j *PassiveSide) Run(ctx context.Context) {

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
			conn := res.conn
			connId++
			connLog := log.
				WithField("connID", connId)
			connLog.
				WithField("addr", conn.RemoteAddr()).
				WithField("client_identity", conn.ClientIdentity()).
				Info("handling connection")
			go func() {
				defer connLog.Info("finished handling connection")
				defer conn.Close()
				ctx := logging.WithSubsystemLoggers(ctx, connLog)
				handleFunc := j.mode.ConnHandleFunc(ctx, conn)
				if handleFunc == nil {
					return
				}
				if err := streamrpc.ServeConn(ctx, conn, j.rpcConf, handleFunc); err != nil {
					log.WithError(err).Error("error serving client")
				}
			}()

		case <-ctx.Done():
			break outer
		}

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
