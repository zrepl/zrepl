package job

import (
	"context"
	"fmt"
	"sort"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/job/wakeup"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/daemon/pruner"
	"github.com/zrepl/zrepl/daemon/snapper"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/zfs"
)

type SnapJob struct {
	name     string
	fsfilter zfs.DatasetFilter
	snapper  *snapper.PeriodicOrManual

	prunerFactory *pruner.LocalPrunerFactory

	promPruneSecs *prometheus.HistogramVec // labels: prune_side

	pruner *pruner.Pruner
}

func (j *SnapJob) Name() string { return j.name }

func (j *SnapJob) Type() Type { return TypeSnap }

func snapJobFromConfig(g *config.Global, in *config.SnapJob) (j *SnapJob, err error) {
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
	j.prunerFactory, err = pruner.NewLocalPrunerFactory(in.Pruning, j.promPruneSecs)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build snapjob pruning rules")
	}
	return j, nil
}

func (j *SnapJob) RegisterMetrics(registerer prometheus.Registerer) {
	registerer.MustRegister(j.promPruneSecs)
}

type SnapJobStatus struct {
	Pruning      *pruner.Report
	Snapshotting *snapper.Report // may be nil
}

func (j *SnapJob) Status() *Status {
	s := &SnapJobStatus{}
	t := j.Type()
	if j.pruner != nil {
		s.Pruning = j.pruner.Report()
	}
	s.Snapshotting = j.snapper.Report()
	return &Status{Type: t, JobSpecific: s}
}

func (j *SnapJob) OwnedDatasetSubtreeRoot() (rfs *zfs.DatasetPath, ok bool) {
	return nil, false
}

func (j *SnapJob) Run(ctx context.Context) {
	log := GetLogger(ctx)
	ctx = logging.WithSubsystemLoggers(ctx, log)

	defer log.Info("job exiting")

	periodicDone := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go j.snapper.Run(ctx, periodicDone)

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

// Adaptor that implements pruner.History around a pruner.Target.
// The ReplicationCursor method is Get-op only and always returns
// the filesystem's most recent version's GUID.
//
// TODO:
// This is a work-around for the current package daemon/pruner
// and package pruning.Snapshot limitation: they require the
//  `Replicated` getter method be present, but obviously,
// a local job like SnapJob can't deliver on that.
// But the pruner.Pruner gives up on an FS if no replication
// cursor is present, which is why this pruner returns the
// most recent filesystem version.
type alwaysUpToDateReplicationCursorHistory struct {
	// the Target passed as Target to BuildLocalPruner
	target pruner.Target
}

var _ pruner.History = (*alwaysUpToDateReplicationCursorHistory)(nil)

func (h alwaysUpToDateReplicationCursorHistory) ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error) {
	if req.GetGet() == nil {
		return nil, fmt.Errorf("unsupported ReplicationCursor request: SnapJob only supports GETting a (faked) cursor")
	}
	fsvReq := &pdu.ListFilesystemVersionsReq{
		Filesystem: req.GetFilesystem(),
	}
	res, err := h.target.ListFilesystemVersions(ctx, fsvReq)
	if err != nil {
		return nil, err
	}
	fsvs := res.GetVersions()
	if len(fsvs) <= 0 {
		return &pdu.ReplicationCursorRes{Result: &pdu.ReplicationCursorRes_Notexist{Notexist: true}}, nil
	}
	// always return must recent version
	sort.Slice(fsvs, func(i, j int) bool {
		return fsvs[i].CreateTXG < fsvs[j].CreateTXG
	})
	mostRecent := fsvs[len(fsvs)-1]
	return &pdu.ReplicationCursorRes{Result: &pdu.ReplicationCursorRes_Guid{Guid: mostRecent.GetGuid()}}, nil
}

func (h alwaysUpToDateReplicationCursorHistory) ListFilesystems(ctx context.Context, req *pdu.ListFilesystemReq) (*pdu.ListFilesystemRes, error) {
	return h.target.ListFilesystems(ctx, req)
}

func (j *SnapJob) doPrune(ctx context.Context) {
	log := GetLogger(ctx)
	ctx = logging.WithSubsystemLoggers(ctx, log)
	sender := endpoint.NewSender(j.fsfilter)
	j.pruner = j.prunerFactory.BuildLocalPruner(ctx, sender, alwaysUpToDateReplicationCursorHistory{sender})
	log.Info("start pruning")
	j.pruner.Prune()
	log.Info("finished pruning")
}
