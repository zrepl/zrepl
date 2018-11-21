package pruner

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/pruning"
	"github.com/zrepl/zrepl/replication/pdu"
	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/util/watchdog"
	"github.com/problame/go-streamrpc"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Try to keep it compatible with gitub.com/zrepl/zrepl/replication.Endpoint
type History interface {
	ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error)
}

type Target interface {
	ListFilesystems(ctx context.Context) ([]*pdu.Filesystem, error)
	ListFilesystemVersions(ctx context.Context, fs string) ([]*pdu.FilesystemVersion, error) // fix depS
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
	promPruneSecs prometheus.Observer
}

type Pruner struct {
	args args

	Progress watchdog.KeepAlive

	mtx sync.RWMutex

	state State

	// State ErrWait|ErrPerm
	sleepUntil time.Time
	err        error

	// State Exec
	execQueue *execQueue
}

type PrunerFactory struct {
	senderRules                    []pruning.KeepRule
	receiverRules                  []pruning.KeepRule
	retryWait                      time.Duration
	considerSnapAtCursorReplicated bool
	promPruneSecs *prometheus.HistogramVec
}

type SinglePrunerFactory struct {
	keepRules                         []pruning.KeepRule
	retryWait                      time.Duration
	considerSnapAtCursorReplicated bool
	promPruneSecs *prometheus.HistogramVec
}

func checkContainsKeep1(rules []pruning.KeepRule) error {
	if len(rules) == 0 {
		return nil //No keep rules means keep all - ok
	}
	for _, e := range rules {
		switch e.(type) {
		case *pruning.KeepLastN:
			return nil
		}
	}
	return errors.New("sender keep rules must contain last_n or be empty so that the last snapshot is definitely kept")
}

func NewSinglePrunerFactory(in config.PruningLocal, promPruneSecs *prometheus.HistogramVec) (*SinglePrunerFactory, error) {
	rules, err := pruning.RulesFromConfig(in.Keep)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build pruning rules")
	}
	considerSnapAtCursorReplicated := false
	f := &SinglePrunerFactory{
		keepRules: rules,
		retryWait: envconst.Duration("ZREPL_PRUNER_RETRY_INTERVAL", 10 * time.Second),
		considerSnapAtCursorReplicated: considerSnapAtCursorReplicated,
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
		senderRules: keepRulesSender,
		receiverRules: keepRulesReceiver,
		retryWait: envconst.Duration("ZREPL_PRUNER_RETRY_INTERVAL", 10 * time.Second),
		considerSnapAtCursorReplicated: considerSnapAtCursorReplicated,
		promPruneSecs: promPruneSecs,
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

func (f *SinglePrunerFactory) BuildSinglePruner(ctx context.Context, target Target, receiver History) *Pruner {
	p := &Pruner{
		args: args{
			ctx,
			target,
			receiver,
			f.keepRules,
			f.retryWait,
			f.considerSnapAtCursorReplicated,
			f.promPruneSecs.WithLabelValues(),
		},
		state: Plan,
	}
	return p
}

//go:generate enumer -type=State
type State int

const (
	Plan State = 1 << iota
	PlanWait
	Exec
	ExecWait
	ErrPerm
	Done
)

func (s State) statefunc() state {
	var statemap = map[State]state{
		Plan:     statePlan,
		PlanWait: statePlanWait,
		Exec:     stateExec,
		ExecWait: stateExecWait,
		ErrPerm:  nil,
		Done:     nil,
	}
	return statemap[s]
}

func (s State) IsTerminal() bool {
	return s.statefunc() == nil
}

type updater func(func(*Pruner)) State
type state func(args *args, u updater) state

func (p *Pruner) Prune() {
	p.prune(p.args)
}

func (p *Pruner) prune(args args) {
	s := p.state.statefunc()
	for s != nil {
		pre := p.state
		s = s(&args, func(f func(*Pruner)) State {
			p.mtx.Lock()
			defer p.mtx.Unlock()
			f(p)
			return p.state
		})
		post := p.state
		GetLogger(args.ctx).
			WithField("transition", fmt.Sprintf("%s=>%s", pre, post)).
			Debug("state transition")
		if err := p.Error(); err != nil {
			GetLogger(args.ctx).
				WithError(p.err).
				WithField("state", post.String()).
				Error("entering error state after error")
		}
	}
}

type Report struct {
	State string
	SleepUntil time.Time
	Error string
	Pending, Completed []FSReport
}

type FSReport struct {
	Filesystem string
	SnapshotList, DestroyList []SnapshotReport
	ErrorCount int
	LastError string
}

type SnapshotReport struct {
	Name string
	Replicated bool
	Date time.Time
}

func (p *Pruner) Report() *Report {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	r := Report{State: p.state.String()}

	if p.state & (PlanWait|ExecWait) != 0 {
		r.SleepUntil = p.sleepUntil
	}
	if p.state & (PlanWait|ExecWait|ErrPerm) != 0 {
		if p.err != nil {
			r.Error = p.err.Error()
		}
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

func (p *Pruner) Error() error {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	if p.state & (PlanWait|ExecWait|ErrPerm) != 0 {
		return p.err
	}
	return nil
}

type fs struct {
	path  string

	// permanent error during planning
	planErr error

	// snapshots presented by target
	// (type snapshot)
	snaps []pruning.Snapshot
	// destroy list returned by pruning.PruneSnapshots(snaps)
	// (type snapshot)
	destroyList []pruning.Snapshot

	mtx sync.RWMutex

	// only during Exec state, also used by execQueue
	execErrLast error
	execErrCount int

}

func (f *fs) Report() FSReport {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	r := FSReport{}
	r.Filesystem = f.path
	r.ErrorCount = f.execErrCount
	if f.planErr != nil {
		r.LastError = f.planErr.Error()
	} else  if f.execErrLast != nil {
		r.LastError = f.execErrLast.Error()
	}

	r.SnapshotList = make([]SnapshotReport, len(f.snaps))
	for i, snap := range f.snaps {
		r.SnapshotList[i] = snap.(snapshot).Report()
	}

	r.DestroyList = make([]SnapshotReport, len(f.destroyList))
	for i, snap := range f.destroyList{
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

type Error interface {
	error
	Temporary() bool
}

var _ Error = net.Error(nil)
var _ Error = streamrpc.Error(nil)

func shouldRetry(e error) bool {
	if neterr, ok := e.(net.Error); ok {
		return neterr.Temporary()
	}
	return false
}

func onErr(u updater, e error) state {
	return u(func(p *Pruner) {
		p.err = e
		if !shouldRetry(e) {
			p.state = ErrPerm
			return
		}
		switch p.state {
		case Plan:
			p.state = PlanWait
		case Exec:
			p.state = ExecWait
		default:
			panic(p.state)
		}
	}).statefunc()
}

func statePlan(a *args, u updater) state {

	ctx, target, receiver := a.ctx, a.target, a.receiver
	var ka *watchdog.KeepAlive
	u(func(pruner *Pruner) {
		ka = &pruner.Progress
	})

	tfss, err := target.ListFilesystems(ctx)
	if err != nil {
		return onErr(u, err)
	}

	pfss := make([]*fs, len(tfss))
	for i, tfs := range tfss {

		l := GetLogger(ctx).WithField("fs", tfs.Path)
		l.Debug("plan filesystem")


		pfs := &fs{
			path:  tfs.Path,
		}
		pfss[i] = pfs

		tfsvs, err := target.ListFilesystemVersions(ctx, tfs.Path)
		if err != nil {
			l.WithError(err).Error("cannot list filesystem versions")
			return onErr(u, err)
		}
		// no progress here since we could run in a live-lock (must have used target AND receiver before progress)

		pfs.snaps = make([]pruning.Snapshot, 0, len(tfsvs))

		rcReq := &pdu.ReplicationCursorReq{
			Filesystem: tfs.Path,
			Op:         &pdu.ReplicationCursorReq_Get{
				Get: &pdu.ReplicationCursorReq_GetOp{},
			},
		}
		rc, err := receiver.ReplicationCursor(ctx, rcReq)
		if err != nil {
			l.WithError(err).Error("cannot get replication cursor")
			return onErr(u, err)
		}
		ka.MadeProgress()
		if rc.GetNotexist()  {
			l.Error("replication cursor does not exist, skipping")
			pfs.destroyList = []pruning.Snapshot{}
			pfs.planErr = fmt.Errorf("replication cursor bookmark does not exist (one successful replication is required before pruning works)")
			continue
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
				err := fmt.Errorf("%s%s has invalid creation date: %s", tfs, tfsv.RelName(), err)
				l.WithError(err).
					WithField("tfsv", tfsv.RelName()).
					Error("error with fileesystem version")
				return onErr(u, err)
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
			err := fmt.Errorf("replication cursor not found in prune target filesystem versions")
			l.Error(err.Error())
			return onErr(u, err)
		}

		// Apply prune rules
		pfs.destroyList = pruning.PruneSnapshots(pfs.snaps, a.rules)
		ka.MadeProgress()
	}

	return u(func(pruner *Pruner) {
		pruner.Progress.MadeProgress()
		pruner.execQueue = newExecQueue(len(pfss))
		for _, pfs := range pfss {
			pruner.execQueue.Put(pfs, nil, false)
		}
		pruner.state = Exec
	}).statefunc()
}

func stateExec(a *args, u updater) state {

	var pfs *fs
	state := u(func(pruner *Pruner) {
		pfs = pruner.execQueue.Pop()
		if pfs == nil {
			nextState := Done
			if pruner.execQueue.HasCompletedFSWithErrors() {
				nextState = ErrPerm
			}
			pruner.state = nextState
			return
		}
	})
	if state != Exec {
		return state.statefunc()
	}

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
		return onErr(u, err)
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
		for i := 0; i < len(destroyFails); i++{
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
		return onErr(u, err)
	}

	return u(func(pruner *Pruner) {
		pruner.Progress.MadeProgress()
	}).statefunc()
}

func stateExecWait(a *args, u updater) state {
	return doWait(Exec, a, u)
}

func statePlanWait(a *args, u updater) state {
	return doWait(Plan, a, u)
}

func doWait(goback State, a *args, u updater) state {
	timer := time.NewTimer(a.retryWait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return u(func(pruner *Pruner) {
			pruner.state = goback
		}).statefunc()
	case <-a.ctx.Done():
		return onErr(u, a.ctx.Err())
	}
}
