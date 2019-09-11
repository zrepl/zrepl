package pruner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/pruning"
	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/util/envconst"
)

// Try to keep it compatible with gitub.com/zrepl/zrepl/endpoint.Endpoint
type History interface {
	ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error)
	ListFilesystems(ctx context.Context, req *pdu.ListFilesystemReq) (*pdu.ListFilesystemRes, error)
}

// Try to keep it compatible with gitub.com/zrepl/zrepl/endpoint.Endpoint
type Target interface {
	ListFilesystems(ctx context.Context, req *pdu.ListFilesystemReq) (*pdu.ListFilesystemRes, error)
	ListFilesystemVersions(ctx context.Context, req *pdu.ListFilesystemVersionsReq) (*pdu.ListFilesystemVersionsRes, error)
	DestroySnapshots(ctx context.Context, req *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error)
}

type Logger = logger.Logger

type contextKey int

const contextKeyLogger contextKey = 0

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLogger, log)
}

func GetLogger(ctx context.Context) Logger {
	if l, ok := ctx.Value(contextKeyLogger).(Logger); ok {
		return l
	}
	return logger.NewNullLogger()
}

type args struct {
	ctx                            context.Context
	target                         Target
	receiver                       History
	rules                          []pruning.KeepRule
	retryWait                      time.Duration
	considerSnapAtCursorReplicated bool
	promPruneSecs                  prometheus.Observer
}

type Pruner struct {
	args args

	mtx sync.RWMutex

	state State

	// State PlanErr
	err error

	// State Exec
	execQueue *execQueue
}

type PrunerFactory struct {
	senderRules                    []pruning.KeepRule
	receiverRules                  []pruning.KeepRule
	retryWait                      time.Duration
	considerSnapAtCursorReplicated bool
	promPruneSecs                  *prometheus.HistogramVec
}

type LocalPrunerFactory struct {
	keepRules     []pruning.KeepRule
	retryWait     time.Duration
	promPruneSecs *prometheus.HistogramVec
}

func NewLocalPrunerFactory(in config.PruningLocal, promPruneSecs *prometheus.HistogramVec) (*LocalPrunerFactory, error) {
	rules, err := pruning.RulesFromConfig(in.Keep)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build pruning rules")
	}
	for _, r := range in.Keep {
		if _, ok := r.Ret.(*config.PruneKeepNotReplicated); ok {
			// rule NotReplicated  for a local pruner doesn't make sense
			// because no replication happens with that job type
			return nil, fmt.Errorf("single-site pruner cannot support `not_replicated` keep rule")
		}
	}
	f := &LocalPrunerFactory{
		keepRules:     rules,
		retryWait:     envconst.Duration("ZREPL_PRUNER_RETRY_INTERVAL", 10*time.Second),
		promPruneSecs: promPruneSecs,
	}
	return f, nil
}

func NewPrunerFactory(in config.PruningSenderReceiver, promPruneSecs *prometheus.HistogramVec) (*PrunerFactory, error) {
	keepRulesReceiver, err := pruning.RulesFromConfig(in.KeepReceiver)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build receiver pruning rules")
	}

	keepRulesSender, err := pruning.RulesFromConfig(in.KeepSender)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build sender pruning rules")
	}

	considerSnapAtCursorReplicated := false
	for _, r := range in.KeepSender {
		knr, ok := r.Ret.(*config.PruneKeepNotReplicated)
		if !ok {
			continue
		}
		considerSnapAtCursorReplicated = considerSnapAtCursorReplicated || !knr.KeepSnapshotAtCursor
	}
	f := &PrunerFactory{
		senderRules:                    keepRulesSender,
		receiverRules:                  keepRulesReceiver,
		retryWait:                      envconst.Duration("ZREPL_PRUNER_RETRY_INTERVAL", 10*time.Second),
		considerSnapAtCursorReplicated: considerSnapAtCursorReplicated,
		promPruneSecs:                  promPruneSecs,
	}
	return f, nil
}

func (f *PrunerFactory) BuildSenderPruner(ctx context.Context, target Target, receiver History) *Pruner {
	p := &Pruner{
		args: args{
			WithLogger(ctx, GetLogger(ctx).WithField("prune_side", "sender")),
			target,
			receiver,
			f.senderRules,
			f.retryWait,
			f.considerSnapAtCursorReplicated,
			f.promPruneSecs.WithLabelValues("sender"),
		},
		state: Plan,
	}
	return p
}

func (f *PrunerFactory) BuildReceiverPruner(ctx context.Context, target Target, receiver History) *Pruner {
	p := &Pruner{
		args: args{
			WithLogger(ctx, GetLogger(ctx).WithField("prune_side", "receiver")),
			target,
			receiver,
			f.receiverRules,
			f.retryWait,
			false, // senseless here anyways
			f.promPruneSecs.WithLabelValues("receiver"),
		},
		state: Plan,
	}
	return p
}

func (f *LocalPrunerFactory) BuildLocalPruner(ctx context.Context, target Target, receiver History) *Pruner {
	p := &Pruner{
		args: args{
			ctx,
			target,
			receiver,
			f.keepRules,
			f.retryWait,
			false, // considerSnapAtCursorReplicated is not relevant for local pruning
			f.promPruneSecs.WithLabelValues("local"),
		},
		state: Plan,
	}
	return p
}

//go:generate enumer -type=State
type State int

const (
	Plan State = 1 << iota
	PlanErr
	Exec
	ExecErr
	Done
)

type updater func(func(*Pruner))

func (p *Pruner) Prune() {
	p.prune(p.args)
}

func (p *Pruner) prune(args args) {
	u := func(f func(*Pruner)) {
		p.mtx.Lock()
		defer p.mtx.Unlock()
		f(p)
	}
	// TODO support automatic retries
	// It is advisable to merge this code with package replication/driver before
	// That will likely require re-modelling struct fs like replication/driver.attempt,
	// including figuring out how to resume a plan after being interrupted by network errors
	// The non-retrying code in this package should move straight to replication/logic.
	doOneAttempt(&args, u)
}

type Report struct {
	State              string
	Error              string
	Pending, Completed []FSReport
}

type FSReport struct {
	Filesystem                string
	SnapshotList, DestroyList []SnapshotReport
	SkipReason                FSSkipReason
	LastError                 string
}

type SnapshotReport struct {
	Name       string
	Replicated bool
	Date       time.Time
}

func (p *Pruner) Report() *Report {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	r := Report{State: p.state.String()}

	if p.err != nil {
		r.Error = p.err.Error()
	}

	if p.execQueue != nil {
		r.Pending, r.Completed = p.execQueue.Report()
	}

	return &r
}

func (p *Pruner) State() State {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	return p.state
}

type fs struct {
	path string

	// permanent error during planning
	planErr        error
	planErrContext string

	// if != "", the fs was skipped for planning and the field
	// contains the reason
	skipReason FSSkipReason

	// snapshots presented by target
	// (type snapshot)
	snaps []pruning.Snapshot
	// destroy list returned by pruning.PruneSnapshots(snaps)
	// (type snapshot)
	destroyList []pruning.Snapshot

	mtx sync.RWMutex

	// only during Exec state, also used by execQueue
	execErrLast error
}

type FSSkipReason string

const (
	NotSkipped                   = ""
	SkipPlaceholder              = "filesystem is placeholder"
	SkipNoCorrespondenceOnSender = "filesystem has no correspondence on sender"
)

func (r FSSkipReason) NotSkipped() bool {
	return r == NotSkipped
}

func (f *fs) Report() FSReport {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	r := FSReport{}
	r.Filesystem = f.path
	r.SkipReason = f.skipReason
	if !r.SkipReason.NotSkipped() {
		return r
	}

	if f.planErr != nil {
		r.LastError = f.planErr.Error()
	} else if f.execErrLast != nil {
		r.LastError = f.execErrLast.Error()
	}

	r.SnapshotList = make([]SnapshotReport, len(f.snaps))
	for i, snap := range f.snaps {
		r.SnapshotList[i] = snap.(snapshot).Report()
	}

	r.DestroyList = make([]SnapshotReport, len(f.destroyList))
	for i, snap := range f.destroyList {
		r.DestroyList[i] = snap.(snapshot).Report()
	}

	return r
}

type snapshot struct {
	replicated bool
	date       time.Time
	fsv        *pdu.FilesystemVersion
}

func (s snapshot) Report() SnapshotReport {
	return SnapshotReport{
		Name:       s.Name(),
		Replicated: s.Replicated(),
		Date:       s.Date(),
	}
}

var _ pruning.Snapshot = snapshot{}

func (s snapshot) Name() string { return s.fsv.Name }

func (s snapshot) Replicated() bool { return s.replicated }

func (s snapshot) Date() time.Time { return s.date }

func doOneAttempt(a *args, u updater) {

	ctx, target, receiver := a.ctx, a.target, a.receiver

	sfssres, err := receiver.ListFilesystems(ctx, &pdu.ListFilesystemReq{})
	if err != nil {
		u(func(p *Pruner) {
			p.state = PlanErr
			p.err = err
		})
		return
	}
	sfss := make(map[string]*pdu.Filesystem)
	for _, sfs := range sfssres.GetFilesystems() {
		sfss[sfs.GetPath()] = sfs
	}

	tfssres, err := target.ListFilesystems(ctx, &pdu.ListFilesystemReq{})
	if err != nil {
		u(func(p *Pruner) {
			p.state = PlanErr
			p.err = err
		})
		return
	}
	tfss := tfssres.GetFilesystems()

	pfss := make([]*fs, len(tfss))
tfss_loop:
	for i, tfs := range tfss {

		l := GetLogger(ctx).WithField("fs", tfs.Path)
		l.Debug("plan filesystem")

		pfs := &fs{
			path: tfs.Path,
		}
		pfss[i] = pfs

		if tfs.GetIsPlaceholder() {
			pfs.skipReason = SkipPlaceholder
			l.WithField("skip_reason", pfs.skipReason).Debug("skipping filesystem")
			continue
		} else if sfs := sfss[tfs.GetPath()]; sfs == nil {
			pfs.skipReason = SkipNoCorrespondenceOnSender
			l.WithField("skip_reason", pfs.skipReason).WithField("sfs", sfs.GetPath()).Debug("skipping filesystem")
			continue
		}

		pfsPlanErrAndLog := func(err error, message string) {
			t := fmt.Sprintf("%T", err)
			pfs.planErr = err
			pfs.planErrContext = message
			l.WithField("orig_err_type", t).WithError(err).Error(fmt.Sprintf("%s: plan error, skipping filesystem", message))
		}

		tfsvsres, err := target.ListFilesystemVersions(ctx, &pdu.ListFilesystemVersionsReq{Filesystem: tfs.Path})
		if err != nil {
			pfsPlanErrAndLog(err, "cannot list filesystem versions")
			continue tfss_loop
		}
		tfsvs := tfsvsres.GetVersions()
		// no progress here since we could run in a live-lock (must have used target AND receiver before progress)

		pfs.snaps = make([]pruning.Snapshot, 0, len(tfsvs))

		rcReq := &pdu.ReplicationCursorReq{
			Filesystem: tfs.Path,
		}
		rc, err := receiver.ReplicationCursor(ctx, rcReq)
		if err != nil {
			pfsPlanErrAndLog(err, "cannot get replication cursor bookmark")
			continue tfss_loop
		}
		if rc.GetNotexist() {
			err := errors.New("replication cursor bookmark does not exist (one successful replication is required before pruning works)")
			pfsPlanErrAndLog(err, "")
			continue tfss_loop
		}

		// scan from older to newer, all snapshots older than cursor are interpreted as replicated
		sort.Slice(tfsvs, func(i, j int) bool {
			return tfsvs[i].CreateTXG < tfsvs[j].CreateTXG
		})

		haveCursorSnapshot := false
		for _, tfsv := range tfsvs {
			if tfsv.Type != pdu.FilesystemVersion_Snapshot {
				continue
			}
			if tfsv.Guid == rc.GetGuid() {
				haveCursorSnapshot = true
			}
		}
		preCursor := haveCursorSnapshot
		for _, tfsv := range tfsvs {
			if tfsv.Type != pdu.FilesystemVersion_Snapshot {
				continue
			}
			creation, err := tfsv.CreationAsTime()
			if err != nil {
				err := fmt.Errorf("%s: %s", tfsv.RelName(), err)
				pfsPlanErrAndLog(err, "fs version with invalid creation date")
				continue tfss_loop
			}
			// note that we cannot use CreateTXG because target and receiver could be on different pools
			atCursor := tfsv.Guid == rc.GetGuid()
			preCursor = preCursor && !atCursor
			pfs.snaps = append(pfs.snaps, snapshot{
				replicated: preCursor || (a.considerSnapAtCursorReplicated && atCursor),
				date:       creation,
				fsv:        tfsv,
			})
		}
		if preCursor {
			pfsPlanErrAndLog(fmt.Errorf("replication cursor not found in prune target filesystem versions"), "")
			continue tfss_loop
		}

		// Apply prune rules
		pfs.destroyList = pruning.PruneSnapshots(pfs.snaps, a.rules)
	}

	u(func(pruner *Pruner) {
		pruner.execQueue = newExecQueue(len(pfss))
		for _, pfs := range pfss {
			pruner.execQueue.Put(pfs, nil, false)
		}
		pruner.state = Exec
	})

	for {
		var pfs *fs
		u(func(pruner *Pruner) {
			pfs = pruner.execQueue.Pop()
		})
		if pfs == nil {
			break
		}
		doOneAttemptExec(a, u, pfs)
	}

	var rep *Report
	{
		// must not hold lock for report
		var pruner *Pruner
		u(func(p *Pruner) {
			pruner = p
		})
		rep = pruner.Report()
	}
	u(func(p *Pruner) {
		if len(rep.Pending) > 0 {
			panic("queue should not have pending items at this point")
		}
		hadErr := false
		for _, fsr := range rep.Completed {
			hadErr = hadErr || fsr.SkipReason.NotSkipped() && fsr.LastError != ""
		}
		if hadErr {
			p.state = ExecErr
		} else {
			p.state = Done
		}
	})

}

// attempts to exec pfs, puts it back into the queue with the result
func doOneAttemptExec(a *args, u updater, pfs *fs) {

	destroyList := make([]*pdu.FilesystemVersion, len(pfs.destroyList))
	for i := range destroyList {
		destroyList[i] = pfs.destroyList[i].(snapshot).fsv
		GetLogger(a.ctx).
			WithField("fs", pfs.path).
			WithField("destroy_snap", destroyList[i].Name).
			Debug("policy destroys snapshot")
	}
	req := pdu.DestroySnapshotsReq{
		Filesystem: pfs.path,
		Snapshots:  destroyList,
	}
	GetLogger(a.ctx).WithField("fs", pfs.path).Debug("destroying snapshots")
	res, err := a.target.DestroySnapshots(a.ctx, &req)
	if err != nil {
		u(func(pruner *Pruner) {
			pruner.execQueue.Put(pfs, err, false)
		})
		return
	}
	// check if all snapshots were destroyed
	destroyResults := make(map[string]*pdu.DestroySnapshotRes)
	for _, fsres := range res.Results {
		destroyResults[fsres.Snapshot.Name] = fsres
	}
	err = nil
	destroyFails := make([]*pdu.DestroySnapshotRes, 0)
	for _, reqDestroy := range destroyList {
		res, ok := destroyResults[reqDestroy.Name]
		if !ok {
			err = fmt.Errorf("missing destroy-result for %s", reqDestroy.RelName())
			break
		} else if res.Error != "" {
			destroyFails = append(destroyFails, res)
		}
	}
	if err == nil && len(destroyFails) > 0 {
		names := make([]string, len(destroyFails))
		pairs := make([]string, len(destroyFails))
		allSame := true
		lastMsg := destroyFails[0].Error
		for i := 0; i < len(destroyFails); i++ {
			allSame = allSame && destroyFails[i].Error == lastMsg
			relname := destroyFails[i].Snapshot.RelName()
			names[i] = relname
			pairs[i] = fmt.Sprintf("(%s: %s)", relname, destroyFails[i].Error)
		}
		if allSame {
			err = fmt.Errorf("destroys failed %s: %s",
				strings.Join(names, ", "), lastMsg)
		} else {
			err = fmt.Errorf("destroys failed: %s", strings.Join(pairs, ", "))
		}
	}
	u(func(pruner *Pruner) {
		pruner.execQueue.Put(pfs, err, err == nil)
	})
	if err != nil {
		GetLogger(a.ctx).WithError(err).Error("target could not destroy snapshots")
		return
	}
}
