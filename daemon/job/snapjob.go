package job

import (
	"context"
	"github.com/pkg/errors"
//	"github.com/problame/go-streamrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/job/reset"
	"github.com/zrepl/zrepl/daemon/job/wakeup"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/daemon/pruner"
	//"github.com/zrepl/zrepl/pruning"
	"github.com/zrepl/zrepl/daemon/snapper"
	"github.com/zrepl/zrepl/daemon/transport/connecter"
	"github.com/zrepl/zrepl/endpoint"
//	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/zfs"
	"sync"
	"time"
)

type snap_ActiveSide struct {
	mode          snap_activeMode
	name          string
	clientFactory *connecter.ClientFactory

	prunerFactory *pruner.SinglePrunerFactory

	//promRepStateSecs *prometheus.HistogramVec // labels: state
	promPruneSecs *prometheus.HistogramVec // labels: prune_side
	//promBytesReplicated *prometheus.CounterVec // labels: filesystem

	tasksMtx sync.Mutex
	tasks    snap_activeSideTasks
}


type snap_activeSideTasks struct {
	state ActiveSideState

	// valid for state ActiveSideReplicating, ActiveSidePruneSender, ActiveSidePruneReceiver, ActiveSideDone
	//replication *replication.Replication
	//replicationCancel context.CancelFunc

	// valid for state ActiveSidePruneSender, ActiveSidePruneReceiver, ActiveSideDone
	//prunerSender, prunerReceiver *pruner.Pruner
	pruner *pruner.Pruner

	// valid for state ActiveSidePruneReceiver, ActiveSideDone
	prunerCancel context.CancelFunc
}

func (a *snap_ActiveSide) Name() string { return a.name }

func (a *snap_ActiveSide) GetPruner(ctx context.Context, sender *endpoint.Sender) (*pruner.Pruner) {
    //p := &pruner.Pruner{ args: pruner.args{ctx, WithLogger(ctx, GetLogger(ctx).WithField("prune_side", "sender")), sender, sender, a.rules, envconst.Duration("ZREPL_PRUNER_RETRY_INTERVAL", 10 * time.Second), true, a.promPruneSecs.WithLabelValues("sender")}, state: pruner.State.Plan,}
    p := a.prunerFactory.BuildSinglePruner(ctx,sender,sender)
    /*if err != nil {
	return nil, errors.Wrap(err, "cannot build receiver pruning rules")
    }*/
    return p
}


func (a *snap_ActiveSide) updateTasks(u func(*snap_activeSideTasks)) snap_activeSideTasks {
	a.tasksMtx.Lock()
	defer a.tasksMtx.Unlock()
	var copy snap_activeSideTasks
	copy = a.tasks
	if u == nil {
		return copy
	}
	u(&copy)
	a.tasks = copy
	return copy
}


type snap_activeMode interface {
	Type() Type
	RunPeriodic(ctx context.Context, wakeUpCommon chan<- struct{})
	//FSFilter() endpoint.FSFilter
	FSFilter() zfs.DatasetFilter
}



type modeSnap struct {
//    fsfilter         endpoint.FSFilter
    fsfilter         zfs.DatasetFilter
    snapper *snapper.PeriodicOrManual
}

func (m *modeSnap) Type() Type { return TypeSnap }

func (m *modeSnap) RunPeriodic(ctx context.Context, wakeUpCommon chan <- struct{}) {
    m.snapper.Run(ctx, wakeUpCommon)
}

func (m *modeSnap) FSFilter() zfs.DatasetFilter {
	return m.fsfilter
}

func modeSnapFromConfig(g *config.Global, in *config.SnapJob) (*modeSnap, error) {
    m := &modeSnap{}
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


func snap_activeSide(g *config.Global, in *config.SnapJob, mode *modeSnap) (j *snap_ActiveSide, err error) {

	j = &snap_ActiveSide{mode: mode}
	j.name = in.Name
/***	j.promRepStateSecs = prometheus.NewHistogramVec(prometheus.HistogramOpts{
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
***/
	j.promPruneSecs = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "zrepl",
		Subsystem:   "pruning",
		Name:        "time",
		Help:        "seconds spent in pruner",
		ConstLabels: prometheus.Labels{"zrepl_job":j.name},
	}, []string{"prune_side"})
	j.prunerFactory, err = pruner.NewSinglePrunerFactory(in.Pruning, j.promPruneSecs)
	//j.rules = pruning.RulesFromConfig(in.Keep)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build snapjob pruning rules")
	}
	return j, nil
}

func (j *snap_ActiveSide) RegisterMetrics(registerer prometheus.Registerer) {
	registerer.MustRegister(j.promPruneSecs)
}

/*func (j *ActiveSide) Name() string { return j.name }

type ActiveSideStatus struct {
	Replication *replication.Report
	PruningSender, PruningReceiver *pruner.Report
}
*/

func (j *snap_ActiveSide) Status() *Status {
	tasks := j.updateTasks(nil)

	s := &ActiveSideStatus{}
	t := j.mode.Type()
	/*if tasks.replication != nil {
		s.Replication = tasks.replication.Report()
	}
	if tasks.prunerSender != nil {*/
	if tasks.pruner != nil {
		s.PruningSender = tasks.pruner.Report()
	}
	/*if tasks.prunerReceiver != nil {
		s.PruningReceiver = tasks.prunerReceiver.Report()
	}*/
	return &Status{Type: t, JobSpecific: s}
}

func (j *snap_ActiveSide) Run(ctx context.Context) {
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

func (j *snap_ActiveSide) do(ctx context.Context) {

	log := GetLogger(ctx)
	ctx = logging.WithSubsystemLoggers(ctx, log)

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

	// The code after this watchdog goroutine is sequential and transitions the state from
	//   ActiveSideReplicating -> ActiveSidePruneSender -> ActiveSidePruneReceiver -> ActiveSideDone
	// If any of those sequential tasks 'gets stuck' (livelock, no progress), the watchdog will eventually
	// cancel its context.
	// If the task is written to support context cancellation, it will return immediately (in permanent error state),
	// and the sequential code above transitions to the next state.
	go func() {

		wdto := envconst.Duration("ZREPL_JOB_WATCHDOG_TIMEOUT", 10*time.Minute)
		jitter := envconst.Duration("ZREPL_JOB_WATCHDOG_JITTER", 1*time.Second)
		// shadowing!
		log := log.WithField("watchdog_timeout", wdto.String())

		log.Debug("starting watchdog")
		defer log.Debug("watchdog stopped")

		t := time.NewTicker(wdto)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C: // fall
			}

			j.updateTasks(func(tasks *snap_activeSideTasks) {
				// Since cancelling a task will cause the sequential code to transition to the next state immediately,
				// we cannot check for its progress right then (no fallthrough).
				// Instead, we return (not continue because we are in a closure) and give the new state another
				// ZREPL_JOB_WATCHDOG_TIMEOUT interval to try make some progress.

				log.WithField("state", tasks.state).Debug("watchdog firing")

				const WATCHDOG_ENVCONST_NOTICE = " (adjust ZREPL_JOB_WATCHDOG_TIMEOUT env variable if inappropriate)"

				switch tasks.state {
				/*case ActiveSideReplicating:
					log.WithField("replication_progress", tasks.replication.Progress.String()).
						Debug("check replication progress")
					if tasks.replication.Progress.CheckTimeout(wdto, jitter) {
						log.Error("replication did not make progress, cancelling" + WATCHDOG_ENVCONST_NOTICE)
						tasks.replicationCancel()
						return
					}*/
				case ActiveSidePruneSender:
					log.WithField("prune_sender_progress", "TEST DEBUG 123").
						Debug("check pruner_sender progress")
					if tasks.pruner.Progress.CheckTimeout(wdto, jitter) {
						log.Error("pruner_sender did not make progress, cancelling" + WATCHDOG_ENVCONST_NOTICE)
						tasks.prunerCancel()
						return
					}
				/*case ActiveSidePruneReceiver:
					log.WithField("prune_receiver_progress", tasks.replication.Progress.String()).
						Debug("check pruner_receiver progress")
					if tasks.prunerReceiver.Progress.CheckTimeout(wdto, jitter) {
						log.Error("pruner_receiver did not make progress, cancelling" + WATCHDOG_ENVCONST_NOTICE)
						tasks.prunerReceiverCancel()
						return
					}*/
				case ActiveSideDone:
					// ignore, ctx will be Done() in a few milliseconds and the watchdog will exit
				default:
					log.WithField("state", tasks.state).
						Error("watchdog implementation error: unknown active side state")
				}
			})

		}
	}()

/*	client, err := j.clientFactory.NewClient()
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
*/
		ctx, localCancel := context.WithCancel(ctx)
		sender := endpoint.NewSender(j.mode.FSFilter())
		tasks := j.updateTasks(func(tasks *snap_activeSideTasks) {
			tasks.pruner = j.GetPruner(ctx, sender)
			tasks.prunerCancel = localCancel
			tasks.state = ActiveSidePruneSender
		})

		log.Info("start pruning sender")
		tasks.pruner.Prune()
		log.Info("finished pruning sender")
		localCancel()
//	}
/*	{
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
*/
	j.updateTasks(func(tasks *snap_activeSideTasks) {
		tasks.state = ActiveSideDone
	})

}

