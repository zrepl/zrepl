package job

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/daemon/logging/trace"

	"github.com/zrepl/zrepl/config"
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
	name   endpoint.JobID
	listen transport.AuthenticatedListenerFactory
}

type passiveMode interface {
	Handler() rpc.Handler
	RunPeriodic(ctx context.Context)
	SnapperReport() *snapper.Report // may be nil
	Type() Type
}

type modeSink struct {
	receiverConfig endpoint.ReceiverConfig
}

func (m *modeSink) Type() Type { return TypeSink }

func (m *modeSink) Handler() rpc.Handler {
	return endpoint.NewReceiver(m.receiverConfig)
}

func (m *modeSink) RunPeriodic(_ context.Context)  {}
func (m *modeSink) SnapperReport() *snapper.Report { return nil }

func modeSinkFromConfig(g *config.Global, in *config.SinkJob, jobID endpoint.JobID) (m *modeSink, err error) {
	m = &modeSink{}

	m.receiverConfig, err = buildReceiverConfig(in, jobID)
	if err != nil {
		return nil, err
	}

	return m, nil
}

type modeSource struct {
	senderConfig *endpoint.SenderConfig
	snapper      *snapper.PeriodicOrManual
}

func modeSourceFromConfig(g *config.Global, in *config.SourceJob, jobID endpoint.JobID) (m *modeSource, err error) {
	// FIXME exact dedup of modePush
	m = &modeSource{}

	m.senderConfig, err = buildSenderConfig(in, jobID)
	if err != nil {
		return nil, errors.Wrap(err, "send options")
	}

	if m.snapper, err = snapper.FromConfig(g, m.senderConfig.FSF, in.Snapshotting); err != nil {
		return nil, errors.Wrap(err, "cannot build snapper")
	}

	return m, nil
}

func (m *modeSource) Type() Type { return TypeSource }

func (m *modeSource) Handler() rpc.Handler {
	return endpoint.NewSender(*m.senderConfig)
}

func (m *modeSource) RunPeriodic(ctx context.Context) {
	m.snapper.Run(ctx, nil)
}

func (m *modeSource) SnapperReport() *snapper.Report {
	return m.snapper.Report()
}

func passiveSideFromConfig(g *config.Global, in *config.PassiveJob, configJob interface{}) (s *PassiveSide, err error) {

	s = &PassiveSide{}

	s.name, err = endpoint.MakeJobID(in.Name)
	if err != nil {
		return nil, errors.Wrap(err, "invalid job name")
	}

	switch v := configJob.(type) {
	case *config.SinkJob:
		s.mode, err = modeSinkFromConfig(g, v, s.name) // shadow
	case *config.SourceJob:
		s.mode, err = modeSourceFromConfig(g, v, s.name) // shadow
	}
	if err != nil {
		return nil, err // no wrapping necessary
	}

	if s.listen, err = fromconfig.ListenerFactoryFromConfig(g, in.Serve); err != nil {
		return nil, errors.Wrap(err, "cannot build listener factory")
	}

	return s, nil
}

func (j *PassiveSide) Name() string { return j.name.String() }

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
	return sink.receiverConfig.RootWithoutClientComponent.Copy(), true
}

func (j *PassiveSide) SenderConfig() *endpoint.SenderConfig {
	source, ok := j.mode.(*modeSource)
	if !ok {
		_ = j.mode.(*modeSink) // make sure we didn't introduce a new job type
		return nil
	}
	return source.senderConfig
}

func (*PassiveSide) RegisterMetrics(registerer prometheus.Registerer) {}

func (j *PassiveSide) Run(ctx context.Context) {
	ctx, endTask := trace.WithTaskAndSpan(ctx, "passive-side-job", j.Name())
	defer endTask()
	log := GetLogger(ctx)
	defer log.Info("job exiting")
	{
		ctx, endTask := trace.WithTask(ctx, "periodic") // shadowing
		defer endTask()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		go j.mode.RunPeriodic(ctx)
	}

	handler := j.mode.Handler()
	if handler == nil {
		panic(fmt.Sprintf("implementation error: j.mode.Handler() returned nil: %#v", j))
	}

	ctxInterceptor := func(handlerCtx context.Context, info rpc.HandlerContextInterceptorData, handler func(ctx context.Context)) {
		// the handlerCtx is clean => need to inherit logging and tracing config from job context
		handlerCtx = logging.WithInherit(handlerCtx, ctx)
		handlerCtx = trace.WithInherit(handlerCtx, ctx)

		handlerCtx, endTask := trace.WithTaskAndSpan(handlerCtx, "handler", fmt.Sprintf("job=%q client=%q method=%q", j.Name(), info.ClientIdentity(), info.FullMethod()))
		defer endTask()
		handler(handlerCtx)
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
