package job

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/daemon/snapper"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/transport/fromconfig"
	"github.com/zrepl/zrepl/zfs"
)

type PassiveSide struct {
	mode   passiveMode
	name   string
	listen transport.AuthenticatedListenerFactory
}

type passiveMode interface {
	Handler() rpc.Handler
	RunPeriodic(ctx context.Context)
	SnapperReport() *snapper.Report // may be nil
	Type() Type
}

type modeSink struct {
	rootDataset *zfs.DatasetPath
}

func (m *modeSink) Type() Type { return TypeSink }

func (m *modeSink) Handler() rpc.Handler {
	return endpoint.NewReceiver(m.rootDataset, true)
}

func (m *modeSink) RunPeriodic(_ context.Context)  {}
func (m *modeSink) SnapperReport() *snapper.Report { return nil }

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
	snapper  *snapper.PeriodicOrManual
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

func (m *modeSource) Handler() rpc.Handler {
	return endpoint.NewSender(m.fsfilter)
}

func (m *modeSource) RunPeriodic(ctx context.Context) {
	m.snapper.Run(ctx, nil)
}

func (m *modeSource) SnapperReport() *snapper.Report {
	return m.snapper.Report()
}

func passiveSideFromConfig(g *config.Global, in *config.PassiveJob, mode passiveMode) (s *PassiveSide, err error) {

	s = &PassiveSide{mode: mode, name: in.Name}
	if s.listen, err = fromconfig.ListenerFactoryFromConfig(g, in.Serve); err != nil {
		return nil, errors.Wrap(err, "cannot build listener factory")
	}

	return s, nil
}

func (j *PassiveSide) Name() string { return j.name }

type PassiveStatus struct {
	Snapper *snapper.Report
}

func (s *PassiveSide) Status() *Status {
	st := &PassiveStatus{
		Snapper: s.mode.SnapperReport(),
	}
	return &Status{Type: s.mode.Type(), JobSpecific: st}
}

func (j *PassiveSide) OwnedDatasetSubtreeRoot() (rfs *zfs.DatasetPath, ok bool) {
	sink, ok := j.mode.(*modeSink)
	if !ok {
		_ = j.mode.(*modeSource) // make sure we didn't introduce a new job type
		return nil, false
	}
	return sink.rootDataset.Copy(), true
}

func (*PassiveSide) RegisterMetrics(registerer prometheus.Registerer) {}

func (j *PassiveSide) Run(ctx context.Context) {

	log := GetLogger(ctx)
	defer log.Info("job exiting")
	ctx = logging.WithSubsystemLoggers(ctx, log)
	{
		ctx, cancel := context.WithCancel(ctx) // shadowing
		defer cancel()
		go j.mode.RunPeriodic(ctx)
	}

	handler := j.mode.Handler()
	if handler == nil {
		panic(fmt.Sprintf("implementation error: j.mode.Handler() returned nil: %#v", j))
	}

	ctxInterceptor := func(handlerCtx context.Context) context.Context {
		return logging.WithSubsystemLoggers(handlerCtx, log)
	}

	rpcLoggers := rpc.GetLoggersOrPanic(ctx) // WithSubsystemLoggers above
	server := rpc.NewServer(handler, rpcLoggers, ctxInterceptor)

	listener, err := j.listen()
	if err != nil {
		log.WithError(err).Error("cannot listen")
		return
	}

	server.Serve(ctx, listener)
}
