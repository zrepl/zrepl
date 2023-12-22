package job

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/daemon/logging/trace"
	"github.com/zrepl/zrepl/util/bandwidthlimit"
	"github.com/zrepl/zrepl/util/nodefault"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/job/trigger"
	"github.com/zrepl/zrepl/daemon/job/wakeup"
	"github.com/zrepl/zrepl/daemon/pruner"
	"github.com/zrepl/zrepl/daemon/snapper"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/zfs"
)

type SnapJob struct {
	name     endpoint.JobID
	fsfilter zfs.DatasetFilter
	snapper  snapper.Snapper

	prunerFactory *pruner.LocalPrunerFactory

	promPruneSecs *prometheus.HistogramVec // labels: prune_side

	prunerMtx sync.Mutex
	pruner    *pruner.Pruner
}

func (j *SnapJob) Name() string { return j.name.String() }

func (j *SnapJob) Type() Type { return TypeSnap }

func snapJobFromConfig(g *config.Global, in *config.SnapJob) (j *SnapJob, err error) {
	j = &SnapJob{}
	fsf, err := filters.DatasetMapFilterFromConfig(in.Filesystems)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build filesystem filter")
	}
	j.fsfilter = fsf

	if j.snapper, err = snapper.FromConfig(g, fsf, in.Snapshotting); err != nil {
		return nil, errors.Wrap(err, "cannot build snapper")
	}
	j.name, err = endpoint.MakeJobID(in.Name)
	if err != nil {
		return nil, errors.Wrap(err, "invalid job name")
	}
	j.promPruneSecs = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "zrepl",
		Subsystem:   "pruning",
		Name:        "time",
		Help:        "seconds spent in pruner",
		ConstLabels: prometheus.Labels{"zrepl_job": j.name.String()},
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
	j.prunerMtx.Lock()
	if j.pruner != nil {
		s.Pruning = j.pruner.Report()
	}
	j.prunerMtx.Unlock()
	r := j.snapper.Report()
	s.Snapshotting = &r
	return &Status{Type: t, JobSpecific: s}
}

func (j *SnapJob) OwnedDatasetSubtreeRoot() (rfs *zfs.DatasetPath, ok bool) {
	return nil, false
}

func (j *SnapJob) SenderConfig() *endpoint.SenderConfig { return nil }

func (j *SnapJob) Run(ctx context.Context) {
	ctx, endTask := trace.WithTaskAndSpan(ctx, "snap-job", j.Name())
	defer endTask()
	log := GetLogger(ctx)

	defer log.Info("job exiting")

	wakeupTrigger := wakeup.Trigger(ctx)

	snapshottingTrigger := trigger.New("periodic")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	periodicCtx, endTask := trace.WithTask(ctx, "snapshotting")
	defer endTask()
	go j.snapper.Run(periodicCtx, snapshottingTrigger)

	triggers := trigger.Empty()
	triggered, endTask := triggers.Spawn(ctx, []trigger.TriggersnapshottingTrigger, wakeupTrigger})
	defer endTask()

	invocationCount := 0
outer:
	for {
		log.Info("wait for wakeups")
		select {
		case <-ctx.Done():
			log.WithError(ctx.Err()).Info("context")
			break outer
		case <-triggered:
		}
		invocationCount++

		invocationCtx, endSpan := trace.WithSpan(ctx, fmt.Sprintf("invocation-%d", invocationCount))
		j.doPrune(invocationCtx)
		endSpan()
	}
}

// Adaptor that implements pruner.History around a pruner.Target.
// The ReplicationCursor method is Get-op only and always returns
// the filesystem's most recent version's GUID.
//
// TODO:
// This is a work-around for the current package daemon/pruner
// and package pruning.Snapshot limitation: they require the
// `Replicated` getter method be present, but obviously,
// a local job like SnapJob can't deliver on that.
// But the pruner.Pruner gives up on an FS if no replication
// cursor is present, which is why this pruner returns the
// most recent filesystem version.
type alwaysUpToDateReplicationCursorHistory struct {
	// the Target passed as Target to BuildLocalPruner
	target pruner.Target
}

var _ pruner.Sender = (*alwaysUpToDateReplicationCursorHistory)(nil)

func (h alwaysUpToDateReplicationCursorHistory) ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error) {
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
	ctx, endSpan := trace.WithSpan(ctx, "snap-job-do-prune")
	defer endSpan()
	log := GetLogger(ctx)
	sender := endpoint.NewSender(endpoint.SenderConfig{
		JobID: j.name,
		FSF:   j.fsfilter,
		// FIXME the following config fields are irrelevant for SnapJob
		// because the endpoint is only used as pruner.Target.
		// However, the implementation requires them to be set.
		Encrypt:        &nodefault.Bool{B: true},
		BandwidthLimit: bandwidthlimit.NoLimitConfig(),
	})
	j.prunerMtx.Lock()
	j.pruner = j.prunerFactory.BuildLocalPruner(ctx, sender, alwaysUpToDateReplicationCursorHistory{sender})
	j.prunerMtx.Unlock()
	log.Info("start pruning")
	j.pruner.Prune()
	log.Info("finished pruning")
}
