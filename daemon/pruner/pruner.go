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
	"net"
	"sort"
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

	mtx sync.RWMutex

	state State

	// State ErrWait|ErrPerm
	sleepUntil time.Time
	err        error

	// State Exec
	prunePending   []*fs
	pruneCompleted []*fs
}

type PrunerFactory struct {
	senderRules                    []pruning.KeepRule
	receiverRules                  []pruning.KeepRule
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
		keepRulesSender,
		keepRulesReceiver,
		10 * time.Second, //FIXME constant
		considerSnapAtCursorReplicated,
		promPruneSecs,
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
	Error string
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

	if p.state & PlanWait|ExecWait != 0 {
		r.SleepUntil = p.sleepUntil
	}
	if p.state & PlanWait|ExecWait|ErrPerm != 0 {
		if p.err != nil {
			r.Error = p.err.Error()
		}
	}

	if p.state & Plan|PlanWait == 0 {
		return &r
	}

	r.Pending = make([]FSReport, len(p.prunePending))
	for i, fs := range p.prunePending{
		r.Pending[i] = fs.Report()
	}
	r.Completed = make([]FSReport, len(p.pruneCompleted))
	for i, fs := range p.pruneCompleted{
		r.Completed[i] = fs.Report()
	}

	return &r
}

type fs struct {
	path  string

	// snapshots presented by target
	// (type snapshot)
	snaps []pruning.Snapshot
	// destroy list returned by pruning.PruneSnapshots(snaps)
	// (type snapshot)
	destroyList []pruning.Snapshot

	mtx sync.RWMutex
	// for Plan
	err error
}

func (f *fs) Update(err error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.err = err
}

func (f *fs) Report() FSReport {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	r := FSReport{}
	r.Filesystem = f.path
	if f.err != nil {
		r.Error = f.err.Error()
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

func shouldRetry(e error) bool {
	switch e.(type) {
	case nil:
		return true
	case net.Error:
		return true
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

	tfss, err := target.ListFilesystems(ctx)
	if err != nil {
		return onErr(u, err)
	}

	pfss := make([]*fs, len(tfss))
fsloop:
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
			if shouldRetry(err) {
				return onErr(u, err)
			}
			pfs.err = err
			continue fsloop
		}
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
			if shouldRetry(err) {
				return onErr(u, err)
			}
			pfs.err = err
			continue fsloop
		}
		if rc.GetError() != "" {
			l.WithField("reqErr", rc.GetError()).Error("cannot get replication cursor")
			pfs.err = fmt.Errorf("%s", rc.GetError())
			continue fsloop
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
				pfs.err = fmt.Errorf("%s%s has invalid creation date: %s", tfs, tfsv.RelName(), err)
				l.WithError(pfs.err).Error("")
				continue fsloop
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
			pfs.err = fmt.Errorf("replication cursor not found in prune target filesystem versions")
			l.WithError(pfs.err).Error("")
			continue fsloop
		}

		// Apply prune rules
		pfs.destroyList = pruning.PruneSnapshots(pfs.snaps, a.rules)

	}

	return u(func(pruner *Pruner) {
		for _, pfs := range pfss {
			if pfs.err != nil {
				pruner.pruneCompleted = append(pruner.pruneCompleted, pfs)
			} else {
				pruner.prunePending = append(pruner.prunePending, pfs)
			}
		}
		pruner.state = Exec
	}).statefunc()
}

func stateExec(a *args, u updater) state {

	var pfs *fs
	state := u(func(pruner *Pruner) {
		if len(pruner.prunePending) == 0 {
			nextState := Done
			for _, pfs := range pruner.pruneCompleted {
				if pfs.err != nil {
					nextState = ErrPerm
				}
			}
			pruner.state = nextState
			return
		}
		pfs = pruner.prunePending[0]
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
	pfs.Update(nil)
	req := pdu.DestroySnapshotsReq{
		Filesystem: pfs.path,
		Snapshots:  destroyList,
	}
	_, err := a.target.DestroySnapshots(a.ctx, &req)
	pfs.Update(err)
	if err != nil && shouldRetry(err) {
		return onErr(u, err)
	}
	// if it's not retryable, treat is like as being done

	return u(func(pruner *Pruner) {
		pruner.pruneCompleted = append(pruner.pruneCompleted, pfs)
		pruner.prunePending = pruner.prunePending[1:]
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
