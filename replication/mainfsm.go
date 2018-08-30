// Package replication implements replication of filesystems with existing
// versions (snapshots) from a sender to a receiver.
package replication

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"sync"
	"time"

	"github.com/zrepl/zrepl/replication/fsrep"
	. "github.com/zrepl/zrepl/replication/internal/diff"
	. "github.com/zrepl/zrepl/replication/internal/queue"
	"github.com/zrepl/zrepl/replication/pdu"
)

//go:generate stringer -type=State
type State uint

const (
	Planning State = 1 << iota
	PlanningError
	Working
	WorkingWait
	Completed
	ContextDone
)

func (s State) rsf() state {
	idx := bits.TrailingZeros(uint(s))
	if idx == bits.UintSize {
		panic(s) // invalid value
	}
	m := []state{
		statePlanning,
		statePlanningError,
		stateWorking,
		stateWorkingWait,
		nil,
		nil,
	}
	return m[idx]
}

// Replication implements the replication of multiple file systems from a Sender to a Receiver.
//
// It is a state machine that is driven by the Drive method
// and provides asynchronous reporting via the Report method (i.e. from another goroutine).
type Replication struct {
	// lock protects all fields of this struct (but not the fields behind pointers!)
	lock sync.Mutex

	state State

	// Working, WorkingWait, Completed, ContextDone
	queue     *ReplicationQueue
	completed []*fsrep.Replication
	active    *ReplicationQueueItemHandle

	// PlanningError
	planningError error

	// ContextDone
	contextError error

	// PlanningError, WorkingWait
	sleepUntil time.Time
}

type Report struct {
	Status    string
	Problem   string
	Completed []*fsrep.Report
	Pending   []*fsrep.Report
	Active    *fsrep.Report
}

func NewReplication() *Replication {
	r := Replication{
		state: Planning,
	}
	return &r
}

// Endpoint represents one side of the replication.
//
// An endpoint is either in Sender or Receiver mode, represented by the correspondingly
// named interfaces defined in this package.
type Endpoint interface {
	// Does not include placeholder filesystems
	ListFilesystems(ctx context.Context) ([]*pdu.Filesystem, error)
	// FIXME document FilteredError handling
	ListFilesystemVersions(ctx context.Context, fs string) ([]*pdu.FilesystemVersion, error) // fix depS
	DestroySnapshots(ctx context.Context, req *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error)
}

type Sender interface {
	Endpoint
	fsrep.Sender
}

type Receiver interface {
	Endpoint
	fsrep.Receiver
}

type FilteredError struct{ fs string }

func NewFilteredError(fs string) *FilteredError {
	return &FilteredError{fs}
}

func (f FilteredError) Error() string { return "endpoint does not allow access to filesystem " + f.fs }

type updater func(func(*Replication)) (newState State)
type state func(ctx context.Context, sender Sender, receiver Receiver, u updater) state

// Drive starts the state machine and returns only after replication has finished (with or without errors).
// The Logger in ctx is used for both debug and error logging, but is not guaranteed to be stable
// or end-user friendly.
// User-facing replication progress reports and can be obtained using the Report method,
// whose output will not change after Drive returns.
//
// FIXME: Drive may be only called once per instance of Replication
func (r *Replication) Drive(ctx context.Context, sender Sender, receiver Receiver) {

	var u updater = func(f func(*Replication)) State {
		r.lock.Lock()
		defer r.lock.Unlock()
		if f != nil {
			f(r)
		}
		return r.state
	}

	var s state = statePlanning
	var pre, post State
	for s != nil {
		preTime := time.Now()
		pre = u(nil)
		s = s(ctx, sender, receiver, u)
		delta := time.Now().Sub(preTime)
		post = u(nil)
		getLogger(ctx).
			WithField("transition", fmt.Sprintf("%s => %s", pre, post)).
			WithField("duration", delta).
			Debug("main state transition")
	}

	getLogger(ctx).
		WithField("final_state", post).
		Debug("main final state")
}

func resolveConflict(conflict error) (path []*pdu.FilesystemVersion, msg string) {
	if noCommonAncestor, ok := conflict.(*ConflictNoCommonAncestor); ok {
		if len(noCommonAncestor.SortedReceiverVersions) == 0 {
			// TODO this is hard-coded replication policy: most recent snapshot as source
			var mostRecentSnap *pdu.FilesystemVersion
			for n := len(noCommonAncestor.SortedSenderVersions) - 1; n >= 0; n-- {
				if noCommonAncestor.SortedSenderVersions[n].Type == pdu.FilesystemVersion_Snapshot {
					mostRecentSnap = noCommonAncestor.SortedSenderVersions[n]
					break
				}
			}
			if mostRecentSnap == nil {
				return nil, "no snapshots available on sender side"
			}
			return []*pdu.FilesystemVersion{mostRecentSnap}, fmt.Sprintf("start replication at most recent snapshot %s", mostRecentSnap.RelName())
		}
	}
	return nil, "no automated way to handle conflict type"
}

func statePlanning(ctx context.Context, sender Sender, receiver Receiver, u updater) state {

	log := getLogger(ctx)

	handlePlanningError := func(err error) state {
		return u(func(r *Replication) {
			r.planningError = err
			r.state = PlanningError
		}).rsf()
	}

	sfss, err := sender.ListFilesystems(ctx)
	if err != nil {
		log.WithError(err).Error("error listing sender filesystems")
		return handlePlanningError(err)
	}

	rfss, err := receiver.ListFilesystems(ctx)
	if err != nil {
		log.WithError(err).Error("error listing receiver filesystems")
		return handlePlanningError(err)
	}

	q := NewReplicationQueue()
	mainlog := log
	for _, fs := range sfss {

		log := mainlog.WithField("filesystem", fs.Path)

		log.Info("assessing filesystem")

		sfsvs, err := sender.ListFilesystemVersions(ctx, fs.Path)
		if err != nil {
			log.WithError(err).Error("cannot get remote filesystem versions")
			return handlePlanningError(err)
		}

		if len(sfsvs) <= 1 {
			err := errors.New("sender does not have any versions")
			log.Error(err.Error())
			q.Add(fsrep.NewReplicationWithPermanentError(fs.Path, err))
			continue
		}

		receiverFSExists := false
		for _, rfs := range rfss {
			if rfs.Path == fs.Path {
				receiverFSExists = true
			}
		}

		var rfsvs []*pdu.FilesystemVersion
		if receiverFSExists {
			rfsvs, err = receiver.ListFilesystemVersions(ctx, fs.Path)
			if err != nil {
				if _, ok := err.(*FilteredError); ok {
					log.Info("receiver ignores filesystem")
					continue
				}
				log.WithError(err).Error("receiver error")
				return handlePlanningError(err)
			}
		} else {
			rfsvs = []*pdu.FilesystemVersion{}
		}

		path, conflict := IncrementalPath(rfsvs, sfsvs)
		if conflict != nil {
			var msg string
			path, msg = resolveConflict(conflict) // no shadowing allowed!
			if path != nil {
				log.WithField("conflict", conflict).Info("conflict")
				log.WithField("resolution", msg).Info("automatically resolved")
			} else {
				log.WithField("conflict", conflict).Error("conflict")
				log.WithField("problem", msg).Error("cannot resolve conflict")
			}
		}
		if path == nil {
			q.Add(fsrep.NewReplicationWithPermanentError(fs.Path, conflict))
			continue
		}

		fsrfsm := fsrep.BuildReplication(fs.Path)
		if len(path) == 1 {
			fsrfsm.AddStep(nil, path[0])
		} else {
			for i := 0; i < len(path)-1; i++ {
				fsrfsm.AddStep(path[i], path[i+1])
			}
		}
		qitem := fsrfsm.Done()
		q.Add(qitem)
	}

	return u(func(r *Replication) {
		r.completed = nil
		r.queue = q
		r.planningError = nil
		r.state = Working
	}).rsf()
}

var RetrySleepDuration = 10 * time.Second // FIXME make constant onfigurable

func statePlanningError(ctx context.Context, sender Sender, receiver Receiver, u updater) state {
	u(func(r *Replication) {
		r.sleepUntil = time.Now().Add(RetrySleepDuration)
	})
	t := time.NewTimer(RetrySleepDuration)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return u(func(r *Replication) {
			r.state = ContextDone
			r.contextError = ctx.Err()
		}).rsf()
	case <-t.C:
		return u(func(r *Replication) {
			r.state = Planning
		}).rsf()
	}
}

func stateWorking(ctx context.Context, sender Sender, receiver Receiver, u updater) state {

	var active *ReplicationQueueItemHandle
	rsfNext := u(func(r *Replication) {
		done, next := r.queue.GetNext()
		r.completed = append(r.completed, done...)
		if next == nil {
			r.state = Completed
		}
		r.active = next
		active = next
	}).rsf()

	if active == nil {
		return rsfNext
	}

	state, nextStepDate := active.GetFSReplication().TakeStep(ctx, sender, receiver)

	return u(func(r *Replication) {
		active.Update(state, nextStepDate)
		r.active = nil
	}).rsf()
}

func stateWorkingWait(ctx context.Context, sender Sender, receiver Receiver, u updater) state {
	u(func(r *Replication) {
		r.sleepUntil = time.Now().Add(RetrySleepDuration)
	})
	t := time.NewTimer(RetrySleepDuration)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return u(func(r *Replication) {
			r.state = ContextDone
			r.contextError = ctx.Err()
		}).rsf()
	case <-t.C:
		return u(func(r *Replication) {
			r.state = Working
		}).rsf()
	}
}

// Report provides a summary of the progress of the Replication,
// i.e., a condensed dump of the internal state machine.
// Report is safe to be called asynchronously while Drive is running.
func (r *Replication) Report() *Report {
	r.lock.Lock()
	defer r.lock.Unlock()

	rep := Report{
		Status: r.state.String(),
	}

	if r.state&(Planning|PlanningError|ContextDone) != 0 {
		switch r.state {
		case PlanningError:
			rep.Problem = r.planningError.Error()
		case ContextDone:
			rep.Problem = r.contextError.Error()
		}
		return &rep
	}

	rep.Pending = make([]*fsrep.Report, 0, r.queue.Len())
	rep.Completed = make([]*fsrep.Report, 0, len(r.completed)) // room for active (potentially)

	var active *fsrep.Replication
	if r.active != nil {
		active = r.active.GetFSReplication()
		rep.Active = active.Report()
	}
	r.queue.Foreach(func(h *ReplicationQueueItemHandle) {
		fsr := h.GetFSReplication()
		if active != fsr {
			rep.Pending = append(rep.Pending, fsr.Report())
		}
	})
	for _, fsr := range r.completed {
		rep.Completed = append(rep.Completed, fsr.Report())
	}

	return &rep
}
