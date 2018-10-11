package job

import (
	"context"
	"github.com/pkg/errors"
	"github.com/problame/go-streamrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/daemon/transport/serve"
	"github.com/zrepl/zrepl/daemon/snapper"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/zfs"
	"path"
)

type PassiveSide struct {
	mode passiveMode
	name     string
	l        serve.ListenerFactory
	rpcConf  *streamrpc.ConnConfig
}

type passiveMode interface {
	ConnHandleFunc(ctx context.Context, conn serve.AuthenticatedConn) streamrpc.HandlerFunc
	RunPeriodic(ctx context.Context)
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

func (m *modeSink) RunPeriodic(_ context.Context) {}

func modeSinkFromConfig(g *config.Global, in *config.SinkJob) (m *modeSink, err error) {
	m = &modeSink{}
	m.rootDataset, err = zfs.NewDatasetPath(in.RootFS)
	if err != nil {
		return nil, errors.New("root dataset is not a valid zfs filesystem path")
	}
	if m.rootDataset.Length() <= 0 {
		return nil, errors.New("root dataset must not be empty") // duplicates error check of receiver
	}
	return m, nil
}

type modeSource struct {
	fsfilter zfs.DatasetFilter
	snapper *snapper.PeriodicOrManual
}

func modeSourceFromConfig(g *config.Global, in *config.SourceJob) (m *modeSource, err error) {
	// FIXME exact dedup of modePush
	m = &modeSource{}
	fsf, err := filters.DatasetMapFilterFromConfig(in.Filesystems)
	if err != nil {
		return nil, errors.Wrap(err, "cannnot build filesystem filter")
	}
	m.fsfilter = fsf

	if m.snapper, err = snapper.FromConfig(g, fsf, in.Snapshotting); err != nil {
		return nil, errors.Wrap(err, "cannot build snapper")
	}

	return m, nil
}

func (m *modeSource) Type() Type { return TypeSource }

func (m *modeSource) ConnHandleFunc(ctx context.Context, conn serve.AuthenticatedConn) streamrpc.HandlerFunc {
	sender := endpoint.NewSender(m.fsfilter)
	h := endpoint.NewHandler(sender)
	return h.Handle
}

func (m *modeSource) RunPeriodic(ctx context.Context) {
	m.snapper.Run(ctx, nil)
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

	{
		ctx, cancel := context.WithCancel(logging.WithSubsystemLoggers(ctx, log)) // shadowing
		defer cancel()
		go j.mode.RunPeriodic(ctx)
	}

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
