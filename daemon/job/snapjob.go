package job

import (
	"context"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/job/wakeup"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/daemon/pruner"
	"github.com/zrepl/zrepl/daemon/snapper"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/zfs"
)

type SnapJob struct {
	name     string
	fsfilter zfs.DatasetFilter
	snapper  *snapper.PeriodicOrManual

	prunerFactory *pruner.SinglePrunerFactory

	promPruneSecs *prometheus.HistogramVec // no labels!

	pruner *pruner.Pruner
}

func (j *SnapJob) Name() string { return j.name }

func (j *SnapJob) Type() Type { return TypeSnap }

func snapJob(g *config.Global, in *config.SnapJob) (j *SnapJob, err error) {
	j = &SnapJob{}
	fsf, err := filters.DatasetMapFilterFromConfig(in.Filesystems)
	if err != nil {
		return nil, errors.Wrap(err, "cannnot build filesystem filter")
	}
	j.fsfilter = fsf

	if j.snapper, err = snapper.FromConfig(g, fsf, in.Snapshotting); err != nil {
		return nil, errors.Wrap(err, "cannot build snapper")
	}
	j.name = in.Name
	j.promPruneSecs = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "zrepl",
		Subsystem:   "pruning",
		Name:        "time",
		Help:        "seconds spent in pruner",
		ConstLabels: prometheus.Labels{"zrepl_job": j.name},
	}, []string{"prune_side"})
	j.prunerFactory, err = pruner.NewSinglePrunerFactory(in.Pruning, j.promPruneSecs)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build snapjob pruning rules")
	}
	return j, nil
}

func (j *SnapJob) RegisterMetrics(registerer prometheus.Registerer) {
	registerer.MustRegister(j.promPruneSecs)
}

type SnapJobStatus struct {
	Pruning *pruner.Report
}

func (j *SnapJob) Status() *Status {
	s := &SnapJobStatus{}
	t := j.Type()
	if j.pruner != nil {
		s.Pruning = j.pruner.Report()
	}
	return &Status{Type: t, JobSpecific: s}
}

func (j *SnapJob) Run(ctx context.Context) {
	log := GetLogger(ctx)
	ctx = logging.WithSubsystemLoggers(ctx, log)

	defer log.Info("job exiting")

	periodicDone := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go 	j.snapper.Run(ctx, periodicDone)

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
		j.doPrune(WithLogger(ctx, invLog))
	}
}

func (j *SnapJob) doPrune(ctx context.Context) {
	log := GetLogger(ctx)
	ctx = logging.WithSubsystemLoggers(ctx, log)
	sender := endpoint.NewSender(j.fsfilter)
	j.pruner = 	j.prunerFactory.BuildSinglePruner(ctx, sender, sender)
	log.Info("start pruning")
	j.pruner.Prune()
	log.Info("finished pruning")
}
