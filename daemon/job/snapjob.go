package job

import (
	"context"
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
	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/zfs"
	"sync"
	"time"
)

type snap_ActiveSide struct {
	mode          snap_activeMode
	name          string

	prunerFactory *pruner.SinglePrunerFactory

	promPruneSecs *prometheus.HistogramVec // labels: prune_side

	tasksMtx sync.Mutex
	tasks    snap_activeSideTasks
}


type snap_activeSideTasks struct {
	state ActiveSideState

	pruner *pruner.Pruner

	prunerCancel context.CancelFunc
}

func (a *snap_ActiveSide) Name() string { return a.name }

func (a *snap_ActiveSide) GetPruner(ctx context.Context, sender *endpoint.Sender) (*pruner.Pruner) {
    p := a.prunerFactory.BuildSinglePruner(ctx,sender,sender)
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
	FSFilter() zfs.DatasetFilter
}



type modeSnap struct {
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
	j.promPruneSecs = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "zrepl",
		Subsystem:   "pruning",
		Name:        "time",
		Help:        "seconds spent in pruner",
		ConstLabels: prometheus.Labels{"zrepl_job":j.name},
	}, []string{"prune_side"})
	j.prunerFactory, err = pruner.NewSinglePrunerFactory(in.Pruning, j.promPruneSecs)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build snapjob pruning rules")
	}
	return j, nil
}

func (j *snap_ActiveSide) RegisterMetrics(registerer prometheus.Registerer) {
	registerer.MustRegister(j.promPruneSecs)
}

func (j *snap_ActiveSide) Status() *Status {
	tasks := j.updateTasks(nil)

	s := &ActiveSideStatus{}
	t := j.mode.Type()
	if tasks.pruner != nil {
		s.PruningSender = tasks.pruner.Report()
	}
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
				case ActiveSidePruneSender:
					log.WithField("prune_sender_progress", "TEST DEBUG 123").
						Debug("check pruner_sender progress")
					if tasks.pruner.Progress.CheckTimeout(wdto, jitter) {
						log.Error("pruner_sender did not make progress, cancelling" + WATCHDOG_ENVCONST_NOTICE)
						tasks.prunerCancel()
						return
					}
				case ActiveSideDone:
					// ignore, ctx will be Done() in a few milliseconds and the watchdog will exit
				default:
					log.WithField("state", tasks.state).
						Error("watchdog implementation error: unknown active side state")
				}
			})

		}
	}()

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
	j.updateTasks(func(tasks *snap_activeSideTasks) {
		tasks.state = ActiveSideDone
	})

}

