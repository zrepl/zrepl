package job

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"

	"github.com/zrepl/zrepl/daemon/logging/trace"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/job/reset"
	"github.com/zrepl/zrepl/daemon/job/wakeup"
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
	name      endpoint.JobID
	connecter transport.Connecter

	prunerFactory *pruner.PrunerFactory

	promRepStateSecs      *prometheus.HistogramVec // labels: state
	promPruneSecs         *prometheus.HistogramVec // labels: prune_side
	promBytesReplicated   *prometheus.CounterVec   // labels: filesystem
	promReplicationErrors prometheus.Gauge

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
	ConnectEndpoints(ctx context.Context, connecter transport.Connecter)
	DisconnectEndpoints()
	SenderReceiver() (logic.Sender, logic.Receiver)
	Type() Type
	PlannerPolicy() logic.PlannerPolicy
	RunPeriodic(ctx context.Context, wakeUpCommon chan<- struct{})
	SnapperReport() *snapper.Report
	ResetConnectBackoff()
}

type modePush struct {
	setupMtx      sync.Mutex
	sender        *endpoint.Sender
	receiver      *rpc.Client
	senderConfig  *endpoint.SenderConfig
	plannerPolicy *logic.PlannerPolicy
	snapper       *snapper.PeriodicOrManual
}

func (m *modePush) ConnectEndpoints(ctx context.Context, connecter transport.Connecter) {
	m.setupMtx.Lock()
	defer m.setupMtx.Unlock()
	if m.receiver != nil || m.sender != nil {
		panic("inconsistent use of ConnectEndpoints and DisconnectEndpoints")
	}
	m.sender = endpoint.NewSender(*m.senderConfig)
	m.receiver = rpc.NewClient(connecter, rpc.GetLoggersOrPanic(ctx))
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

func (m *modePush) PlannerPolicy() logic.PlannerPolicy { return *m.plannerPolicy }

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

func modePushFromConfig(g *config.Global, in *config.PushJob, jobID endpoint.JobID) (*modePush, error) {
	m := &modePush{}
	var err error

	m.senderConfig, err = buildSenderConfig(in, jobID)
	if err != nil {
		return nil, errors.Wrap(err, "sender config")
	}

	replicationConfig, err := logic.ReplicationConfigFromConfig(in.Replication)
	if err != nil {
		return nil, errors.Wrap(err, "field `replication`")
	}

	m.plannerPolicy = &logic.PlannerPolicy{
		EncryptedSend:     logic.TriFromBool(in.Send.Encrypted),
		ReplicationConfig: replicationConfig,
	}

	if m.snapper, err = snapper.FromConfig(g, m.senderConfig.FSF, in.Snapshotting); err != nil {
		return nil, errors.Wrap(err, "cannot build snapper")
	}

	return m, nil
}

type modePull struct {
	setupMtx       sync.Mutex
	receiver       *endpoint.Receiver
	receiverConfig endpoint.ReceiverConfig
	sender         *rpc.Client
	plannerPolicy  *logic.PlannerPolicy
	interval       config.PositiveDurationOrManual
}

func (m *modePull) ConnectEndpoints(ctx context.Context, connecter transport.Connecter) {
	m.setupMtx.Lock()
	defer m.setupMtx.Unlock()
	if m.receiver != nil || m.sender != nil {
		panic("inconsistent use of ConnectEndpoints and DisconnectEndpoints")
	}
	m.receiver = endpoint.NewReceiver(m.receiverConfig)
	m.sender = rpc.NewClient(connecter, rpc.GetLoggersOrPanic(ctx))
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

func (m *modePull) PlannerPolicy() logic.PlannerPolicy { return *m.plannerPolicy }

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

func modePullFromConfig(g *config.Global, in *config.PullJob, jobID endpoint.JobID) (m *modePull, err error) {
	m = &modePull{}
	m.interval = in.Interval

	replicationConfig, err := logic.ReplicationConfigFromConfig(in.Replication)
	if err != nil {
		return nil, errors.Wrap(err, "field `replication`")
	}

	m.plannerPolicy = &logic.PlannerPolicy{
		EncryptedSend:     logic.DontCare,
		ReplicationConfig: replicationConfig,
	}

	m.receiverConfig, err = buildReceiverConfig(in, jobID)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func activeSide(g *config.Global, in *config.ActiveJob, configJob interface{}) (j *ActiveSide, err error) {

	j = &ActiveSide{}
	j.name, err = endpoint.MakeJobID(in.Name)
	if err != nil {
		return nil, errors.Wrap(err, "invalid job name")
	}

	switch v := configJob.(type) {
	case *config.PushJob:
		j.mode, err = modePushFromConfig(g, v, j.name) // shadow
	case *config.PullJob:
		j.mode, err = modePullFromConfig(g, v, j.name) // shadow
	default:
		panic(fmt.Sprintf("implementation error: unknown job type %T", v))
	}
	if err != nil {
		return nil, err // no wrapping required
	}

	j.promRepStateSecs = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "zrepl",
		Subsystem:   "replication",
		Name:        "state_time",
		Help:        "seconds spent during replication",
		ConstLabels: prometheus.Labels{"zrepl_job": j.name.String()},
	}, []string{"state"})
	j.promBytesReplicated = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   "zrepl",
		Subsystem:   "replication",
		Name:        "bytes_replicated",
		Help:        "number of bytes replicated from sender to receiver per filesystem",
		ConstLabels: prometheus.Labels{"zrepl_job": j.name.String()},
	}, []string{"filesystem"})

	j.promReplicationErrors = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace:   "zrepl",
		Subsystem:   "replication",
		Name:        "filesystem_errors",
		Help:        "number of filesystems that failed replication in the latest replication attempt, or -1 if the job failed before enumerating the filesystems",
		ConstLabels: prometheus.Labels{"zrepl_job": j.name.String()},
	})

	j.connecter, err = fromconfig.ConnecterFromConfig(g, in.Connect)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build client")
	}

	j.promPruneSecs = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "zrepl",
		Subsystem:   "pruning",
		Name:        "time",
		Help:        "seconds spent in pruner",
		ConstLabels: prometheus.Labels{"zrepl_job": j.name.String()},
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
	registerer.MustRegister(j.promReplicationErrors)
}

func (j *ActiveSide) Name() string { return j.name.String() }

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
	return pull.receiverConfig.RootWithoutClientComponent.Copy(), true
}

func (j *ActiveSide) SenderConfig() *endpoint.SenderConfig {
	push, ok := j.mode.(*modePush)
	if !ok {
		_ = j.mode.(*modePull) // make sure we didn't introduce a new job type
		return nil
	}
	return push.senderConfig
}

// The active side of a replication uses one end (sender or receiver)
// directly by method invocation, without going through a transport that
// provides a client identity.
// However, in order to avoid the need to distinguish between direct-method-invocating
// clients and RPC client, we use an invalid client identity as a sentinel value.
func FakeActiveSideDirectMethodInvocationClientIdentity(jobId endpoint.JobID) string {
	return fmt.Sprintf("<local><active><job><client><identity><job=%q>", jobId.String())
}

func (j *ActiveSide) Run(ctx context.Context) {
	ctx, endTask := trace.WithTaskAndSpan(ctx, "active-side-job", j.Name())
	defer endTask()

	ctx = context.WithValue(ctx, endpoint.ClientIdentityKey, FakeActiveSideDirectMethodInvocationClientIdentity(j.name))

	log := GetLogger(ctx)

	defer log.Info("job exiting")

	periodicDone := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	periodicCtx, endTask := trace.WithTask(ctx, "periodic")
	defer endTask()
	go j.mode.RunPeriodic(periodicCtx, periodicDone)

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
		invocationCtx, endSpan := trace.WithSpan(ctx, fmt.Sprintf("invocation-%d", invocationCount))
		j.do(invocationCtx)
		endSpan()
	}
}

func (j *ActiveSide) do(ctx context.Context) {

	j.mode.ConnectEndpoints(ctx, j.connecter)
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
		ctx, endSpan := trace.WithSpan(ctx, "replication")
		ctx, repCancel := context.WithCancel(ctx)
		var repWait driver.WaitFunc
		j.updateTasks(func(tasks *activeSideTasks) {
			// reset it
			*tasks = activeSideTasks{}
			tasks.replicationCancel = func() { repCancel(); endSpan() }
			tasks.replicationReport, repWait = replication.Do(
				ctx, logic.NewPlanner(j.promRepStateSecs, j.promBytesReplicated, sender, receiver, j.mode.PlannerPolicy()),
			)
			tasks.state = ActiveSideReplicating
		})
		GetLogger(ctx).Info("start replication")
		repWait(true) // wait blocking
		repCancel()   // always cancel to free up context resources

		replicationReport := j.tasks.replicationReport()
		j.promReplicationErrors.Set(float64(replicationReport.GetFailedFilesystemsCountInLatestAttempt()))

		endSpan()
	}

	{
		select {
		case <-ctx.Done():
			return
		default:
		}
		ctx, endSpan := trace.WithSpan(ctx, "prune_sender")
		ctx, senderCancel := context.WithCancel(ctx)
		tasks := j.updateTasks(func(tasks *activeSideTasks) {
			tasks.prunerSender = j.prunerFactory.BuildSenderPruner(ctx, sender, sender)
			tasks.prunerSenderCancel = func() { senderCancel(); endSpan() }
			tasks.state = ActiveSidePruneSender
		})
		GetLogger(ctx).Info("start pruning sender")
		tasks.prunerSender.Prune()
		GetLogger(ctx).Info("finished pruning sender")
		senderCancel()
		endSpan()
	}
	{
		select {
		case <-ctx.Done():
			return
		default:
		}
		ctx, endSpan := trace.WithSpan(ctx, "prune_recever")
		ctx, receiverCancel := context.WithCancel(ctx)
		tasks := j.updateTasks(func(tasks *activeSideTasks) {
			tasks.prunerReceiver = j.prunerFactory.BuildReceiverPruner(ctx, receiver, sender)
			tasks.prunerReceiverCancel = func() { receiverCancel(); endSpan() }
			tasks.state = ActiveSidePruneReceiver
		})
		GetLogger(ctx).Info("start pruning receiver")
		tasks.prunerReceiver.Prune()
		GetLogger(ctx).Info("finished pruning receiver")
		receiverCancel()
		endSpan()
	}

	j.updateTasks(func(tasks *activeSideTasks) {
		tasks.state = ActiveSideDone
	})

}
