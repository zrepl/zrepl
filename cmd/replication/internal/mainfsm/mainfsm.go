package mainfsm

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"sync"
	"time"

	. "github.com/zrepl/zrepl/cmd/replication/common"
	"github.com/zrepl/zrepl/cmd/replication/pdu"
	"github.com/zrepl/zrepl/cmd/replication/internal/fsfsm"
	. "github.com/zrepl/zrepl/cmd/replication/internal/mainfsm/queue"
)

//go:generate stringer -type=ReplicationState
type ReplicationState uint

const (
	Planning ReplicationState = 1 << iota
	PlanningError
	Working
	WorkingWait
	Completed
	ContextDone
)

func (s ReplicationState) rsf() replicationStateFunc {
	idx := bits.TrailingZeros(uint(s))
	if idx == bits.UintSize {
		panic(s) // invalid value
	}
	m := []replicationStateFunc{
		rsfPlanning,
		rsfPlanningError,
		rsfWorking,
		rsfWorkingWait,
		nil,
		nil,
	}
	return m[idx]
}

type Replication struct {
	// lock protects all fields of this struct (but not the fields behind pointers!)
	lock sync.Mutex

	state ReplicationState

	// Working, WorkingWait, Completed, ContextDone
	queue     *ReplicationQueue
	completed []*fsfsm.FSReplication
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
	Completed []*fsfsm.FilesystemReplicationReport
	Pending   []*fsfsm.FilesystemReplicationReport
	Active    *fsfsm.FilesystemReplicationReport
}


func NewReplication() *Replication {
	r := Replication{
		state: Planning,
	}
	return &r
}

type replicationUpdater func(func(*Replication)) (newState ReplicationState)
type replicationStateFunc func(context.Context, EndpointPair, replicationUpdater) replicationStateFunc

func (r *Replication) Drive(ctx context.Context, ep EndpointPair) {

	var u replicationUpdater = func(f func(*Replication)) ReplicationState {
		r.lock.Lock()
		defer r.lock.Unlock()
		if f != nil {
			f(r)
		}
		return r.state
	}

	var s replicationStateFunc = rsfPlanning
	var pre, post ReplicationState
	for s != nil {
		preTime := time.Now()
		pre = u(nil)
		s = s(ctx, ep, u)
		delta := time.Now().Sub(preTime)
		post = u(nil)
		GetLogger(ctx).
			WithField("transition", fmt.Sprintf("%s => %s", pre, post)).
			WithField("duration", delta).
			Debug("main state transition")
	}

	GetLogger(ctx).
		WithField("final_state", post).
		Debug("main final state")
}

func resolveConflict(conflict error) (path []*pdu.FilesystemVersion, msg string) {
	if noCommonAncestor, ok := conflict.(*ConflictNoCommonAncestor); ok {
		if len(noCommonAncestor.SortedReceiverVersions) == 0 {
			// FIXME hard-coded replication policy: most recent
			// snapshot as source
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

func rsfPlanning(ctx context.Context, ep EndpointPair, u replicationUpdater) replicationStateFunc {

	log := GetLogger(ctx)

	handlePlanningError := func(err error) replicationStateFunc {
		return u(func(r *Replication) {
			r.planningError = err
			r.state = PlanningError
		}).rsf()
	}

	sfss, err := ep.Sender().ListFilesystems(ctx)
	if err != nil {
		log.WithError(err).Error("error listing sender filesystems")
		return handlePlanningError(err)
	}

	rfss, err := ep.Receiver().ListFilesystems(ctx)
	if err != nil {
		log.WithError(err).Error("error listing receiver filesystems")
		return handlePlanningError(err)
	}

	q := NewReplicationQueue()
	mainlog := log
	for _, fs := range sfss {

		log := mainlog.WithField("filesystem", fs.Path)

		log.Info("assessing filesystem")

		sfsvs, err := ep.Sender().ListFilesystemVersions(ctx, fs.Path)
		if err != nil {
			log.WithError(err).Error("cannot get remote filesystem versions")
			return handlePlanningError(err)
		}

		if len(sfsvs) <= 1 {
			err := errors.New("sender does not have any versions")
			log.Error(err.Error())
			q.Add(fsfsm.NewFSReplicationWithPermanentError(fs.Path, err))
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
			rfsvs, err = ep.Receiver().ListFilesystemVersions(ctx, fs.Path)
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
			q.Add(fsfsm.NewFSReplicationWithPermanentError(fs.Path, conflict))
			continue
		}

		fsrfsm := fsfsm.BuildFSReplication(fs.Path)
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

func rsfPlanningError(ctx context.Context, ep EndpointPair, u replicationUpdater) replicationStateFunc {
	sleepTime := 10 * time.Second
	u(func(r *Replication) {
		r.sleepUntil = time.Now().Add(sleepTime)
	})
	t := time.NewTimer(sleepTime) // FIXME make constant onfigurable
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

func rsfWorking(ctx context.Context, ep EndpointPair, u replicationUpdater) replicationStateFunc {

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

	state, nextStepDate := active.GetFSReplication().TakeStep(ctx, ep)

	return u(func(r *Replication) {
		active.Update(state, nextStepDate)
		r.active = nil
	}).rsf()
}

func rsfWorkingWait(ctx context.Context, ep EndpointPair, u replicationUpdater) replicationStateFunc {
	sleepTime := 10 * time.Second
	u(func(r *Replication) {
		r.sleepUntil = time.Now().Add(sleepTime)
	})
	t := time.NewTimer(sleepTime)
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

	rep.Pending = make([]*fsfsm.FilesystemReplicationReport, 0, r.queue.Len())
	rep.Completed = make([]*fsfsm.FilesystemReplicationReport, 0, len(r.completed)) // room for active (potentially)

	var active *fsfsm.FSReplication
	if r.active != nil {
		active = r.active.GetFSReplication()
		rep.Active = active.Report()
	}
	r.queue.Foreach(func (h *ReplicationQueueItemHandle){
		fsr := h.GetFSReplication()
		if active != fsr {
			rep.Pending = append(rep.Pending, fsr.Report())
		}
	})
	for _, fsr := range r.completed {
		rep.Completed = append(rep.Completed,  fsr.Report())
	}

	return &rep
}

