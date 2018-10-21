package job

import (
	"context"
	"github.com/pkg/errors"
	"github.com/problame/go-streamrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/job/reset"
	"github.com/zrepl/zrepl/daemon/job/wakeup"
	"github.com/zrepl/zrepl/daemon/transport/connecter"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/pruner"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/util/watchdog"
	"github.com/zrepl/zrepl/zfs"
	"sync"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/daemon/snapper"
	"time"
)

type ActiveSide struct {
	mode          activeMode
	name          string
	clientFactory *connecter.ClientFactory

	prunerFactory *pruner.PrunerFactory


	promRepStateSecs *prometheus.HistogramVec // labels: state
	promPruneSecs *prometheus.HistogramVec // labels: prune_side
	promBytesReplicated *prometheus.CounterVec // labels: filesystem

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
	replication *replication.Replication
	replicationCancel context.CancelFunc

	// valid for state ActiveSidePruneSender, ActiveSidePruneReceiver, ActiveSideDone
	prunerSender, prunerReceiver *pruner.Pruner

	// valid for state ActiveSidePruneReceiver, ActiveSideDone
	prunerSenderCancel, prunerReceiverCancel context.CancelFunc
}

func (a *ActiveSide) updateTasks(u func(*activeSideTasks)) activeSideTasks {
	a.tasksMtx.Lock()
	defer a.tasksMtx.Unlock()
	var copy activeSideTasks
	copy = a.tasks
	if u == nil {
		return copy
	}
	u(&copy)
	a.tasks = copy
	return copy
}

type activeMode interface {
	SenderReceiver(client *streamrpc.Client) (replication.Sender, replication.Receiver, error)
	Type() Type
	RunPeriodic(ctx context.Context, wakeUpCommon chan<- struct{})
}

type modePush struct {
	fsfilter         endpoint.FSFilter
	snapper *snapper.PeriodicOrManual
}

func (m *modePush) SenderReceiver(client *streamrpc.Client) (replication.Sender, replication.Receiver, error) {
	sender := endpoint.NewSender(m.fsfilter)
	receiver := endpoint.NewRemote(client)
	return sender, receiver, nil
}

func (m *modePush) Type() Type { return TypePush }

func (m *modePush) RunPeriodic(ctx context.Context, wakeUpCommon chan <- struct{}) {
	m.snapper.Run(ctx, wakeUpCommon)
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
	rootFS   *zfs.DatasetPath
	interval time.Duration
}

func (m *modePull) SenderReceiver(client *streamrpc.Client) (replication.Sender, replication.Receiver, error) {
	sender := endpoint.NewRemote(client)
	receiver, err := endpoint.NewReceiver(m.rootFS)
	return sender, receiver, err
}

func (*modePull) Type() Type { return TypePull }

func (m *modePull) RunPeriodic(ctx context.Context, wakeUpCommon chan<- struct{}) {
	t := time.NewTicker(m.interval)
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

func modePullFromConfig(g *config.Global, in *config.PullJob) (m *modePull, err error) {
	m = &modePull{}
	if in.Interval <= 0 {
		return nil, errors.New("interval must be positive")
	}
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
		ConstLabels: prometheus.Labels{"zrepl_job":j.name},
	}, []string{"state"})
	j.promBytesReplicated = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   "zrepl",
		Subsystem:   "replication",
		Name:        "bytes_replicated",
		Help:        "number of bytes replicated from sender to receiver per filesystem",
		ConstLabels: prometheus.Labels{"zrepl_job":j.name},
	}, []string{"filesystem"})

	j.clientFactory, err = connecter.FromConfig(g, in.Connect)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build client")
	}

	j.promPruneSecs = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "zrepl",
		Subsystem:   "pruning",
		Name:        "time",
		Help:        "seconds spent in pruner",
		ConstLabels: prometheus.Labels{"zrepl_job":j.name},
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
	Replication *replication.Report
	PruningSender, PruningReceiver *pruner.Report
}

func (j *ActiveSide) Status() *Status {
	tasks := j.updateTasks(nil)

	s := &ActiveSideStatus{}
	t := j.mode.Type()
	if tasks.replication != nil {
		s.Replication = tasks.replication.Report()
	}
	if tasks.prunerSender != nil {
		s.PruningSender = tasks.prunerSender.Report()
	}
	if tasks.prunerReceiver != nil {
		s.PruningReceiver = tasks.prunerReceiver.Report()
	}
	return &Status{Type: t, JobSpecific: s}
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

	// allow cancellation of an invocation (this function)
	ctx, cancelThisRun := context.WithCancel(ctx)
	defer cancelThisRun()
	runDone := make(chan struct{})
	defer close(runDone)
	go func() {
		select {
		case <-runDone:
		case <-reset.Wait(ctx):
			log.Info("reset received, cancelling current invocation")
			cancelThisRun()
		case <-ctx.Done():
		}
	}()

	// watchdog
	go func() {
		// if no progress after 1 minute, kill the task
		wdto := envconst.Duration("ZREPL_JOB_WATCHDOG_TIMEOUT", 1*time.Minute)
		log.WithField("watchdog_timeout", wdto.String()).Debug("starting watchdog")

		t := time.NewTicker(wdto)
		defer t.Stop()

		var (
			rep, prunerSender, prunerReceiver watchdog.Progress
		)
		for {
			select {
			case <-runDone:
				return
			case <-ctx.Done():
				return
			case <-t.C: // fall
			}

			log := log.WithField("watchdog_timeout", wdto.String()) // shadowing!

			j.updateTasks(func(tasks *activeSideTasks) {
				if tasks.replication != nil &&
					!tasks.replication.Progress.ExpectProgress(&rep) &&
					!tasks.replication.State().IsTerminal() {
					log.Error("replication did not make progress, cancelling")
					tasks.replicationCancel()
				}
				if tasks.prunerSender != nil &&
					!tasks.prunerSender.Progress.ExpectProgress(&prunerSender) &&
					!tasks.prunerSender.State().IsTerminal() {
					log.Error("pruner:sender did not make progress, cancelling")
					tasks.prunerSenderCancel()
				}
				if tasks.prunerReceiver != nil &&
					!tasks.prunerReceiver.Progress.ExpectProgress(&prunerReceiver) &&
					!tasks.prunerReceiver.State().IsTerminal() {
					log.Error("pruner:receiver did not make progress, cancelling")
					tasks.prunerReceiverCancel()
				}
			})
			log.WithField("replication_progress", rep.String()).
				WithField("pruner_sender_progress", prunerSender.String()).
				WithField("pruner_receiver_progress", prunerReceiver.String()).
				Debug("watchdog did run")

		}
	}()

	client, err := j.clientFactory.NewClient()
	if err != nil {
		log.WithError(err).Error("factory cannot instantiate streamrpc client")
	}
	defer client.Close(ctx)

	sender, receiver, err := j.mode.SenderReceiver(client)

	{
		select {
		case <-ctx.Done():
			return
		default:
		}
		ctx, repCancel := context.WithCancel(ctx)
		tasks := j.updateTasks(func(tasks *activeSideTasks) {
			// reset it
			*tasks = activeSideTasks{}
			tasks.replicationCancel = repCancel
			tasks.replication = replication.NewReplication(j.promRepStateSecs, j.promBytesReplicated)
			tasks.state = ActiveSideReplicating
		})
		log.Info("start replication")
		tasks.replication.Drive(ctx, sender, receiver)
		repCancel() // always cancel to free up context resources
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
