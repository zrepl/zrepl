package job

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/job/reset"
	"github.com/zrepl/zrepl/daemon/job/wakeup"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/daemon/pruner"
	"github.com/zrepl/zrepl/daemon/snapper"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/replication/driver"
	"github.com/zrepl/zrepl/replication/logic"
	"github.com/zrepl/zrepl/replication/report"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/transport/fromconfig"
	"github.com/zrepl/zrepl/zfs"
)

type ActiveSide struct {
	mode      activeMode
	name      string
	connecter transport.Connecter

	prunerFactory *pruner.PrunerFactory

	promRepStateSecs    *prometheus.HistogramVec // labels: state
	promPruneSecs       *prometheus.HistogramVec // labels: prune_side
	promBytesReplicated *prometheus.CounterVec   // labels: filesystem

	tasksMtx sync.Mutex
	tasks    activeSideTasks
}

//go:generate enumer -type=ActiveSideState
type ActiveSideState int

const (
	ActiveSideReplicating ActiveSideState = 1 << iota
	ActiveSidePruneSender
	ActiveSidePruneReceiver
	ActiveSideDone // also errors
)

type activeSideTasks struct {
	state ActiveSideState

	// valid for state ActiveSideReplicating, ActiveSidePruneSender, ActiveSidePruneReceiver, ActiveSideDone
	replicationReport driver.ReportFunc
	replicationCancel context.CancelFunc

	// valid for state ActiveSidePruneSender, ActiveSidePruneReceiver, ActiveSideDone
	prunerSender, prunerReceiver *pruner.Pruner

	// valid for state ActiveSidePruneReceiver, ActiveSideDone
	prunerSenderCancel, prunerReceiverCancel context.CancelFunc
}

func (a *ActiveSide) updateTasks(u func(*activeSideTasks)) activeSideTasks {
	a.tasksMtx.Lock()
	defer a.tasksMtx.Unlock()
	copy := a.tasks
	if u == nil {
		return copy
	}
	u(&copy)
	a.tasks = copy
	return copy
}

type activeMode interface {
	ConnectEndpoints(rpcLoggers rpc.Loggers, connecter transport.Connecter)
	DisconnectEndpoints()
	SenderReceiver() (logic.Sender, logic.Receiver)
	Type() Type
	RunPeriodic(ctx context.Context, wakeUpCommon chan<- struct{})
	SnapperReport() *snapper.Report
	ResetConnectBackoff()
}

type modePush struct {
	setupMtx sync.Mutex
	sender   *endpoint.Sender
	receiver *rpc.Client
	fsfilter endpoint.FSFilter
	snapper  *snapper.PeriodicOrManual
}

func (m *modePush) ConnectEndpoints(loggers rpc.Loggers, connecter transport.Connecter) {
	m.setupMtx.Lock()
	defer m.setupMtx.Unlock()
	if m.receiver != nil || m.sender != nil {
		panic("inconsistent use of ConnectEndpoints and DisconnectEndpoints")
	}
	m.sender = endpoint.NewSender(m.fsfilter)
	m.receiver = rpc.NewClient(connecter, loggers)
}

func (m *modePush) DisconnectEndpoints() {
	m.setupMtx.Lock()
	defer m.setupMtx.Unlock()
	m.receiver.Close()
	m.sender = nil
	m.receiver = nil
}

func (m *modePush) SenderReceiver() (logic.Sender, logic.Receiver) {
	m.setupMtx.Lock()
	defer m.setupMtx.Unlock()
	return m.sender, m.receiver
}

func (m *modePush) Type() Type { return TypePush }

func (m *modePush) RunPeriodic(ctx context.Context, wakeUpCommon chan<- struct{}) {
	m.snapper.Run(ctx, wakeUpCommon)
}

func (m *modePush) SnapperReport() *snapper.Report {
	return m.snapper.Report()
}

func (m *modePush) ResetConnectBackoff() {
	m.setupMtx.Lock()
	defer m.setupMtx.Unlock()
	if m.receiver != nil {
		m.receiver.ResetConnectBackoff()
	}
}

func modePushFromConfig(g *config.Global, in *config.PushJob) (*modePush, error) {
	m := &modePush{}
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

type modePull struct {
	setupMtx sync.Mutex
	receiver *endpoint.Receiver
	sender   *rpc.Client
	rootFS   *zfs.DatasetPath
	interval config.PositiveDurationOrManual
}

func (m *modePull) ConnectEndpoints(loggers rpc.Loggers, connecter transport.Connecter) {
	m.setupMtx.Lock()
	defer m.setupMtx.Unlock()
	if m.receiver != nil || m.sender != nil {
		panic("inconsistent use of ConnectEndpoints and DisconnectEndpoints")
	}
	m.receiver = endpoint.NewReceiver(m.rootFS, false)
	m.sender = rpc.NewClient(connecter, loggers)
}

func (m *modePull) DisconnectEndpoints() {
	m.setupMtx.Lock()
	defer m.setupMtx.Unlock()
	m.sender.Close()
	m.sender = nil
	m.receiver = nil
}

func (m *modePull) SenderReceiver() (logic.Sender, logic.Receiver) {
	m.setupMtx.Lock()
	defer m.setupMtx.Unlock()
	return m.sender, m.receiver
}

func (*modePull) Type() Type { return TypePull }

func (m *modePull) RunPeriodic(ctx context.Context, wakeUpCommon chan<- struct{}) {
	if m.interval.Manual {
		GetLogger(ctx).Info("manual pull configured, periodic pull disabled")
		// "waiting for wakeups" is printed in common ActiveSide.do
		return
	}
	t := time.NewTicker(m.interval.Interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			select {
			case wakeUpCommon <- struct{}{}:
			default:
				GetLogger(ctx).
					WithField("pull_interval", m.interval).
					Warn("pull job took longer than pull interval")
				wakeUpCommon <- struct{}{} // block anyways, to queue up the wakeup
			}
		case <-ctx.Done():
			return
		}
	}
}

func (m *modePull) SnapperReport() *snapper.Report {
	return nil
}

func (m *modePull) ResetConnectBackoff() {
	m.setupMtx.Lock()
	defer m.setupMtx.Unlock()
	if m.sender != nil {
		m.sender.ResetConnectBackoff()
	}
}

func modePullFromConfig(g *config.Global, in *config.PullJob) (m *modePull, err error) {
	m = &modePull{}
	m.interval = in.Interval

	m.rootFS, err = zfs.NewDatasetPath(in.RootFS)
	if err != nil {
		return nil, errors.New("RootFS is not a valid zfs filesystem path")
	}
	if m.rootFS.Length() <= 0 {
		return nil, errors.New("RootFS must not be empty") // duplicates error check of receiver
	}

	return m, nil
}

func activeSide(g *config.Global, in *config.ActiveJob, mode activeMode) (j *ActiveSide, err error) {

	j = &ActiveSide{mode: mode}
	j.name = in.Name
	j.promRepStateSecs = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "zrepl",
		Subsystem:   "replication",
		Name:        "state_time",
		Help:        "seconds spent during replication",
		ConstLabels: prometheus.Labels{"zrepl_job": j.name},
	}, []string{"state"})
	j.promBytesReplicated = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   "zrepl",
		Subsystem:   "replication",
		Name:        "bytes_replicated",
		Help:        "number of bytes replicated from sender to receiver per filesystem",
		ConstLabels: prometheus.Labels{"zrepl_job": j.name},
	}, []string{"filesystem"})

	j.connecter, err = fromconfig.ConnecterFromConfig(g, in.Connect)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build client")
	}

	j.promPruneSecs = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "zrepl",
		Subsystem:   "pruning",
		Name:        "time",
		Help:        "seconds spent in pruner",
		ConstLabels: prometheus.Labels{"zrepl_job": j.name},
	}, []string{"prune_side"})
	j.prunerFactory, err = pruner.NewPrunerFactory(in.Pruning, j.promPruneSecs)
	if err != nil {
		return nil, err
	}

	return j, nil
}

func (j *ActiveSide) RegisterMetrics(registerer prometheus.Registerer) {
	registerer.MustRegister(j.promRepStateSecs)
	registerer.MustRegister(j.promPruneSecs)
	registerer.MustRegister(j.promBytesReplicated)
}

func (j *ActiveSide) Name() string { return j.name }

type ActiveSideStatus struct {
	Replication                    *report.Report
	PruningSender, PruningReceiver *pruner.Report
	Snapshotting                   *snapper.Report
}

func (j *ActiveSide) Status() *Status {
	tasks := j.updateTasks(nil)

	s := &ActiveSideStatus{}
	t := j.mode.Type()
	if tasks.replicationReport != nil {
		s.Replication = tasks.replicationReport()
	}
	if tasks.prunerSender != nil {
		s.PruningSender = tasks.prunerSender.Report()
	}
	if tasks.prunerReceiver != nil {
		s.PruningReceiver = tasks.prunerReceiver.Report()
	}
	s.Snapshotting = j.mode.SnapperReport()
	return &Status{Type: t, JobSpecific: s}
}

func (j *ActiveSide) OwnedDatasetSubtreeRoot() (rfs *zfs.DatasetPath, ok bool) {
	pull, ok := j.mode.(*modePull)
	if !ok {
		_ = j.mode.(*modePush) // make sure we didn't introduce a new job type
		return nil, false
	}
	return pull.rootFS.Copy(), true
}

func (j *ActiveSide) Run(ctx context.Context) {
	log := GetLogger(ctx)
	ctx = logging.WithSubsystemLoggers(ctx, log)

	defer log.Info("job exiting")

	periodicDone := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go j.mode.RunPeriodic(ctx, periodicDone)

	invocationCount := 0
outer:
	for {
		log.Info("wait for wakeups")
		select {
		case <-ctx.Done():
			log.WithError(ctx.Err()).Info("context")
			break outer

		case <-wakeup.Wait(ctx):
			j.mode.ResetConnectBackoff()
		case <-periodicDone:
		}
		invocationCount++
		invLog := log.WithField("invocation", invocationCount)
		j.do(WithLogger(ctx, invLog))
	}
}

func (j *ActiveSide) do(ctx context.Context) {

	log := GetLogger(ctx)
	ctx = logging.WithSubsystemLoggers(ctx, log)
	loggers := rpc.GetLoggersOrPanic(ctx) // filled by WithSubsystemLoggers
	j.mode.ConnectEndpoints(loggers, j.connecter)
	defer j.mode.DisconnectEndpoints()

	// allow cancellation of an invocation (this function)
	ctx, cancelThisRun := context.WithCancel(ctx)
	defer cancelThisRun()
	go func() {
		select {
		case <-reset.Wait(ctx):
			log.Info("reset received, cancelling current invocation")
			cancelThisRun()
		case <-ctx.Done():
		}
	}()

	sender, receiver := j.mode.SenderReceiver()

	{
		select {
		case <-ctx.Done():
			return
		default:
		}
		ctx, repCancel := context.WithCancel(ctx)
		var repWait driver.WaitFunc
		j.updateTasks(func(tasks *activeSideTasks) {
			// reset it
			*tasks = activeSideTasks{}
			tasks.replicationCancel = repCancel
			tasks.replicationReport, repWait = replication.Do(
				ctx, logic.NewPlanner(j.promRepStateSecs, j.promBytesReplicated, sender, receiver),
			)
			tasks.state = ActiveSideReplicating
		})
		log.Info("start replication")
		repWait(true) // wait blocking
		repCancel()   // always cancel to free up context resources
	}

	{
		select {
		case <-ctx.Done():
			return
		default:
		}
		ctx, senderCancel := context.WithCancel(ctx)
		tasks := j.updateTasks(func(tasks *activeSideTasks) {
			tasks.prunerSender = j.prunerFactory.BuildSenderPruner(ctx, sender, sender)
			tasks.prunerSenderCancel = senderCancel
			tasks.state = ActiveSidePruneSender
		})
		log.Info("start pruning sender")
		tasks.prunerSender.Prune()
		log.Info("finished pruning sender")
		senderCancel()
	}
	{
		select {
		case <-ctx.Done():
			return
		default:
		}
		ctx, receiverCancel := context.WithCancel(ctx)
		tasks := j.updateTasks(func(tasks *activeSideTasks) {
			tasks.prunerReceiver = j.prunerFactory.BuildReceiverPruner(ctx, receiver, sender)
			tasks.prunerReceiverCancel = receiverCancel
			tasks.state = ActiveSidePruneReceiver
		})
		log.Info("start pruning receiver")
		tasks.prunerReceiver.Prune()
		log.Info("finished pruning receiver")
		receiverCancel()
	}

	j.updateTasks(func(tasks *activeSideTasks) {
		tasks.state = ActiveSideDone
	})

}
