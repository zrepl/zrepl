package replication

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"time"
)

//go:generate stringer -type=ReplicationState
type ReplicationState int

const (
	Planning ReplicationState = 1 << iota
	PlanningError
	Working
	WorkingWait
	Completed
	ContextDone
)

type replicationQueueItem struct {
	retriesSinceLastError int
	fsr                   *FSReplication
}

type Replication struct {

	// lock protects all fields of this struct (but not the fields behind pointers!)
	lock sync.Mutex

	state ReplicationState

	// Working / WorkingWait

	pending, completed []*replicationQueueItem
	active             *replicationQueueItem

	// PlanningError
	planningError error

	// ContextDone
	contextError error

	sleepUntil time.Time
}

//go:generate stringer -type=FSReplicationState
type FSReplicationState int

const (
	FSQueued FSReplicationState = 1 << iota
	FSActive
	FSRetryWait
	FSPermanentError
	FSCompleted
)

type FSReplication struct {
	lock sync.Mutex
	state              FSReplicationState
	fs                 *Filesystem
	permanentError     error
	completed, pending []*FSReplicationStep
	active             *FSReplicationStep
}

func newReplicationQueueItemPermanentError(fs *Filesystem, err error) *replicationQueueItem {
	return &replicationQueueItem{0, &FSReplication{
		state:          FSPermanentError,
		fs:             fs,
		permanentError: err,
	}}
}

type replicationQueueItemBuilder struct {
	r     *FSReplication
	steps []*FSReplicationStep
}

func buildNewFSReplication(fs *Filesystem) *replicationQueueItemBuilder {
	return &replicationQueueItemBuilder{
		r: &FSReplication{
			fs:      fs,
			pending: make([]*FSReplicationStep, 0),
		},
	}
}

func (b *replicationQueueItemBuilder) AddStep(from, to *FilesystemVersion) *replicationQueueItemBuilder {
	step := &FSReplicationStep{
		state: StepPending,
		fsrep: b.r,
		from:  from,
		to:    to,
	}
	b.r.pending = append(b.r.pending, step)
	return b
}

func (b *replicationQueueItemBuilder) Complete() *replicationQueueItem {
	if len(b.r.pending) > 0 {
		b.r.state = FSQueued
	} else {
		b.r.state = FSCompleted
	}
	r := b.r
	return &replicationQueueItem{0, r}
}

//go:generate stringer -type=FSReplicationStepState
type FSReplicationStepState int

const (
	StepPending FSReplicationStepState = iota
	StepRetry
	StepPermanentError
	StepCompleted
)

type FSReplicationStep struct {
	// only protects state, err
	// from, to and fsrep are assumed to be immutable
	lock sync.Mutex

	state    FSReplicationStepState
	from, to *FilesystemVersion
	fsrep    *FSReplication

	// both retry and permanent error
	err error
}

func (r *Replication) Drive(ctx context.Context, ep EndpointPair, retryNow chan struct{}) {
	for r.state&(Completed|ContextDone) == 0 {
		pre := r.state
		preTime := time.Now()
		r.doDrive(ctx, ep, retryNow)
		delta := time.Now().Sub(preTime)
		post := r.state
		getLogger(ctx).
			WithField("transition", fmt.Sprintf("%s => %s", pre, post)).
			WithField("duration", delta).
			Debug("main state transition")
		now := time.Now()
		sleepDuration := r.sleepUntil.Sub(now)
		if sleepDuration > 100*time.Millisecond {
			getLogger(ctx).
				WithField("duration", sleepDuration).
				WithField("wakeup_at", r.sleepUntil).
				Error("sleeping until next attempt")
			timer := time.NewTimer(sleepDuration)
			select {
			case <-timer.C:
			case <-ctx.Done():
			case <-retryNow:
			}
			timer.Stop()
		}
	}
}

func (r *Replication) doDrive(ctx context.Context, ep EndpointPair, retryNow chan struct{}) {

	switch r.state {

	case Planning:
		r.tryBuildPlan(ctx, ep)

	case PlanningError:
		r.sleepUntil = time.Now().Add(10 * time.Second) // FIXME constant make configurable

	case Working:
		withLocks := func(f func()) {
			r.lock.Lock()
			defer r.lock.Unlock()
			f()
		}
		withLocks(func() {	
			if r.active == nil {

				if len(r.pending) == 0 {
					r.state = Completed
					return
				}

				sort.Slice(r.pending, func(i, j int) bool {
					a, b := r.pending[i], r.pending[j]
					statePrio := func(x *replicationQueueItem) int {
						if x.fsr.state&(FSQueued|FSRetryWait) == 0 {
							panic(x)
						}
						if x.fsr.state == FSQueued {
							return 0
						} else {
							return 1
						}
					}
					aprio, bprio := statePrio(a), statePrio(b)
					if aprio != bprio {
						return aprio < bprio
					}
					// now we know they are the same state
					if a.fsr.state == FSQueued {
						return a.fsr.nextStepDate().Before(b.fsr.nextStepDate())
					}
					if a.fsr.state == FSRetryWait {
						return a.retriesSinceLastError < b.retriesSinceLastError
					}
					panic("should not be reached")
				})

				r.active = r.pending[0]
				r.pending = r.pending[1:]
			}

			if r.active.fsr.state == FSRetryWait {
				r.state = WorkingWait
				return
			}
			if r.active.fsr.state != FSQueued {
				panic(r.active)
			}
		})

		if r.active == nil {
			return
		}

		fsState := r.active.fsr.drive(ctx, ep)

		withLocks(func() {

			if fsState&FSQueued != 0 {
				r.active.retriesSinceLastError = 0
			} else if fsState&FSRetryWait != 0 {
				r.active.retriesSinceLastError++
			} else if fsState&(FSPermanentError|FSCompleted) != 0 {
				r.completed = append(r.completed, r.active)
				r.active = nil
			} else {
				panic(r.active)
			}
		})

	case WorkingWait:
		r.sleepUntil = time.Now().Add(10 * time.Second) // FIXME make configurable

	default:
		panic(r.state)
	}
}

func (r *Replication) tryBuildPlan(ctx context.Context, ep EndpointPair) ReplicationState {

	log := getLogger(ctx)

	updateLock := func() func() {
		r.lock.Lock()
		return func() {
			r.lock.Unlock()
		}
	}

	planningError := func(err error) ReplicationState {
		defer updateLock()()
		r.state = PlanningError
		r.planningError = err
		return r.state
	}

	sfss, err := ep.Sender().ListFilesystems(ctx)
	if err != nil {
		log.WithError(err).Error("error listing sender filesystems")
		return planningError(err)
	}

	rfss, err := ep.Receiver().ListFilesystems(ctx)
	if err != nil {
		log.WithError(err).Error("error listing receiver filesystems")
		return planningError(err)
	}

	pending := make([]*replicationQueueItem, 0, len(sfss))
	completed := make([]*replicationQueueItem, 0, len(sfss))
	mainlog := log
	for _, fs := range sfss {

		log := mainlog.WithField("filesystem", fs.Path)

		log.Info("assessing filesystem")

		sfsvs, err := ep.Sender().ListFilesystemVersions(ctx, fs.Path)
		if err != nil {
			log.WithError(err).Error("cannot get remote filesystem versions")
			return planningError(err)
		}

		if len(sfsvs) <= 1 {
			err := errors.New("sender does not have any versions")
			log.Error(err.Error())
			completed = append(completed, newReplicationQueueItemPermanentError(fs, err))
			continue
		}

		receiverFSExists := false
		for _, rfs := range rfss {
			if rfs.Path == fs.Path {
				receiverFSExists = true
			}
		}

		var rfsvs []*FilesystemVersion
		if receiverFSExists {
			rfsvs, err = ep.Receiver().ListFilesystemVersions(ctx, fs.Path)
			if err != nil {
				if _, ok := err.(FilteredError); ok {
					log.Info("receiver ignores filesystem")
					continue
				}
				log.WithError(err).Error("receiver error")
				return planningError(err)
			}
		} else {
			rfsvs = []*FilesystemVersion{}
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
			completed = append(completed, newReplicationQueueItemPermanentError(fs, conflict))
			continue
		}

		builder := buildNewFSReplication(fs)
		if len(path) == 1 {
			builder.AddStep(nil, path[0])
		} else {
			for i := 0; i < len(path)-1; i++ {
				builder.AddStep(path[i], path[i+1])
			}
		}
		qitem := builder.Complete()
		switch qitem.fsr.state {
		case FSCompleted:
			completed = append(completed, qitem)
		case FSQueued:
			pending = append(pending, qitem)
		default:
			panic(qitem)
		}

	}


	defer updateLock()()
	r.completed = completed
	r.pending = pending
	r.state = Working
	r.planningError = nil
	return r.state
}

// caller must have exclusive access to f
func (f *FSReplication) nextStepDate() time.Time {
	if f.state != FSQueued {
		panic(f)
	}
	ct, err := f.pending[0].to.CreationAsTime()
	if err != nil {
		panic(err) // FIXME
	}
	return ct
}

func (f *FSReplication) drive(ctx context.Context, ep EndpointPair) FSReplicationState {
	f.lock.Lock()
	defer f.lock.Unlock()
	for f.state&(FSRetryWait|FSPermanentError|FSCompleted) == 0 {
		pre := f.state
		preTime := time.Now()
		f.doDrive(ctx, ep)
		delta := time.Now().Sub(preTime)
		post := f.state
		getLogger(ctx).
			WithField("transition", fmt.Sprintf("%s => %s", pre, post)).
			WithField("duration", delta).
			Debug("fsr state transition")
	}
	return f.state
}

// caller must hold f.lock
func (f *FSReplication) doDrive(ctx context.Context, ep EndpointPair) FSReplicationState {
	switch f.state {
	case FSPermanentError:
		fallthrough
	case FSCompleted:
		return f.state
	case FSRetryWait:
		f.state = FSQueued
		return f.state
	case FSQueued:
		if f.active == nil {
			if len(f.pending) == 0 {
				f.state = FSCompleted
				return f.state
			}
			f.active = f.pending[0]
			f.pending = f.pending[1:]
		}
		f.state = FSActive
		return f.state

	case FSActive:
		var stepState FSReplicationStepState
		func() { // drop lock during long call
			f.lock.Unlock()
			defer f.lock.Lock()
			stepState = f.active.do(ctx, ep)
		}()
		switch stepState {
		case StepCompleted:
			f.completed = append(f.completed, f.active)
			f.active = nil
			if len(f.pending) > 0 {
				f.state = FSQueued
			} else {
				f.state = FSCompleted
			}
		case StepRetry:
			f.state = FSRetryWait
		case StepPermanentError:
			f.state = FSPermanentError
		}
		return f.state
	}

	panic(f)
}

func (s *FSReplicationStep) do(ctx context.Context, ep EndpointPair) FSReplicationStepState {

	fs := s.fsrep.fs

	log := getLogger(ctx).
		WithField("filesystem", fs.Path).
		WithField("step", s.String())

	updateStateError := func(err error) FSReplicationStepState {
		s.lock.Lock()
		defer s.lock.Unlock()

		s.err = err
		switch err {
		case io.EOF:
			fallthrough
		case io.ErrUnexpectedEOF:
			fallthrough
		case io.ErrClosedPipe:
			s.state = StepRetry
			return s.state
		}
		if _, ok := err.(net.Error); ok {
			s.state = StepRetry
			return s.state
		}
		s.state = StepPermanentError
		return s.state
	}

	updateStateCompleted := func() FSReplicationStepState {
		s.lock.Lock()
		defer s.lock.Unlock()
		s.err = nil
		s.state = StepCompleted
		return s.state
	}

	// FIXME refresh fs resume token
	fs.ResumeToken = ""

	var sr *SendReq
	if fs.ResumeToken != "" {
		sr = &SendReq{
			Filesystem:  fs.Path,
			ResumeToken: fs.ResumeToken,
		}
	} else if s.from == nil {
		sr = &SendReq{
			Filesystem: fs.Path,
			From:       s.to.RelName(), // FIXME fix protocol to use To, like zfs does internally
		}
	} else {
		sr = &SendReq{
			Filesystem: fs.Path,
			From:       s.from.RelName(),
			To:         s.to.RelName(),
		}
	}

	log.WithField("request", sr).Debug("initiate send request")
	sres, sstream, err := ep.Sender().Send(ctx, sr)
	if err != nil {
		log.WithError(err).Error("send request failed")
		return updateStateError(err)
	}
	if sstream == nil {
		err := errors.New("send request did not return a stream, broken endpoint implementation")
		return updateStateError(err)
	}

	rr := &ReceiveReq{
		Filesystem:       fs.Path,
		ClearResumeToken: !sres.UsedResumeToken,
	}
	log.WithField("request", rr).Debug("initiate receive request")
	err = ep.Receiver().Receive(ctx, rr, sstream)
	if err != nil {
		log.WithError(err).Error("receive request failed (might also be error on sender)")
		sstream.Close()
		// This failure could be due to
		// 	- an unexpected exit of ZFS on the sending side
		//  - an unexpected exit of ZFS on the receiving side
		//  - a connectivity issue
		return updateStateError(err)
	}
	log.Info("receive finished")
	return updateStateCompleted()

}

func (s *FSReplicationStep) String() string {
	if s.from == nil { // FIXME: ZFS semantics are that to is nil on non-incremental send
		return fmt.Sprintf("%s%s (full)", s.fsrep.fs.Path, s.to.RelName())
	} else {
		return fmt.Sprintf("%s(%s => %s)", s.fsrep.fs.Path, s.from.RelName(), s.to.RelName())
	}
}
