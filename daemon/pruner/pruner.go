package pruner

import (
	"context"
	"github.com/zrepl/zrepl/pruning"
	"github.com/zrepl/zrepl/replication/pdu"
	"sync"
	"time"
	"fmt"
	"net"
	"github.com/zrepl/zrepl/logger"
)

// Try to keep it compatible with gitub.com/zrepl/zrepl/replication.Endpoint
type Receiver interface {
	HasFilesystemVersion(ctx context.Context, fs string, version *pdu.FilesystemVersion) (bool, error)
}

type Target interface {
	ListFilesystems(ctx context.Context) ([]*pdu.Filesystem, error)
	ListFilesystemVersions(ctx context.Context, fs string) ([]*pdu.FilesystemVersion, error) // fix depS
	DestroySnapshots(ctx context.Context, fs string, snaps []*pdu.FilesystemVersion) ([]*pdu.FilesystemVersion, error)
}

type Logger = logger.Logger

type contextKey int

const contextKeyLogger contextKey = 0

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLogger, log)
}

func getLogger(ctx context.Context) Logger {
	if l, ok := ctx.Value(contextKeyLogger).(Logger); ok {
		return l
	}
	return logger.NewNullLogger()
}

type args struct {
	ctx context.Context
	target Target
	receiver Receiver
	rules []pruning.KeepRule
	retryWait  time.Duration
}

type Pruner struct {

	args args

	mtx sync.RWMutex

	state State

	// State ErrWait|ErrPerm
	sleepUntil time.Time
	err        error

	// State Exec
	prunePending []fs
	pruneCompleted []fs

}

func NewPruner(retryWait time.Duration, target Target, receiver Receiver, rules []pruning.KeepRule) *Pruner {
	p := &Pruner{
		args: args{nil, target, receiver, rules, retryWait}, // ctx is filled in Prune()
		state: Plan,
	}
	return p
}

//go:generate stringer -type=State
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

func (p *Pruner) Prune(ctx context.Context) {
	p.args.ctx = ctx
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
		getLogger(args.ctx).
			WithField("transition", fmt.Sprintf("%s=>%s", pre, post)).
			Debug("state transition")
	}
}

func (p *Pruner) Report() interface{} {
	return nil // FIXME TODO
}

type fs struct {
	path string
	snaps []pruning.Snapshot

	mtx sync.RWMutex
	// for Plan
	err error
}

func (f* fs) Update(err error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.err = err
}

type snapshot struct {
	replicated bool
	date time.Time
	fsv *pdu.FilesystemVersion
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
		case Plan: p.state = PlanWait
		case Exec: p.state = ExecWait
		default: panic(p.state)
		}
	}).statefunc()
}

func statePlan(a *args, u updater) state {

	ctx, target, receiver := a.ctx, a.target, a.receiver

	tfss, err := target.ListFilesystems(ctx)
	if err != nil {
		return onErr(u, err)
	}

	pfss := make([]fs, len(tfss))
	for i, tfs := range tfss {
		tfsvs, err := target.ListFilesystemVersions(ctx, tfs.Path)
		if err != nil {
			return onErr(u, err)
		}

		pfs := fs{
			path:  tfs.Path,
			snaps: make([]pruning.Snapshot, 0, len(tfsvs)),
		}

		for _, tfsv := range tfsvs {
			if tfsv.Type != pdu.FilesystemVersion_Snapshot {
				continue
			}
			creation, err := tfsv.CreationAsTime()
			if err != nil {
				return onErr(u, fmt.Errorf("%s%s has invalid creation date: %s", tfs, tfsv.RelName(), err))
			}
			replicated, err := receiver.HasFilesystemVersion(ctx, tfs.Path, tfsv)
			if err != nil && shouldRetry(err) {
				return onErr(u, err)
			} else if err != nil {
				pfs.err = err
				pfs.snaps = nil
				break
			}
			pfs.snaps = append(pfs.snaps, snapshot{
				replicated: replicated,
				date:       creation,
				fsv: tfsv,
			})

		}

		pfss[i] = pfs

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

	var pfs fs
	state := u(func(pruner *Pruner) {
		if len(pruner.prunePending) == 0 {
			pruner.state = Done
			return
		}
		pfs = pruner.prunePending[0]
	})
	if state != Exec {
		return state.statefunc()
	}

	destroyListI := pruning.PruneSnapshots(pfs.snaps, a.rules)
	destroyList := make([]*pdu.FilesystemVersion, len(destroyListI))
	for i := range destroyList {
		destroyList[i] = destroyListI[i].(snapshot).fsv
	}
	pfs.Update(nil)
	_, err := a.target.DestroySnapshots(a.ctx, pfs.path, destroyList)
	pfs.Update(err)
	if err !=  nil && shouldRetry(err) {
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
