package replication

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"time"
)

//go:generate stringer -type=ReplicationState
type ReplicationState int

const (
	Planning ReplicationState = iota
	PlanningError
	Working
	WorkingWait
	Completed
	ContextDone
)

type Replication struct {
	state ReplicationState

	// Working / WorkingWait

	pending, completed []*FSReplication

	// PlanningError
	planningError error

	// ContextDone
	contextError error
}

type FSReplicationState int

//go:generate stringer -type=FSReplicationState
const (
	FSQueued FSReplicationState = 1 << iota
	FSActive
	FSRetry
	FSPermanentError
	FSCompleted
)

type FSReplication struct {
	state              FSReplicationState
	fs                 *Filesystem
	permanentError	   error
	retryAt            time.Time
	completed, pending []*FSReplicationStep
}

func newFSReplicationPermanentError(fs *Filesystem, err error) *FSReplication {
	return &FSReplication{
		state: FSPermanentError,
		fs: fs,
		permanentError: err,
	}
}

type FSReplicationBuilder struct {
	r *FSReplication
	steps []*FSReplicationStep
}

func buildNewFSReplication(fs *Filesystem) *FSReplicationBuilder {
	return &FSReplicationBuilder{
		r: &FSReplication{
			fs: fs,
			pending: make([]*FSReplicationStep, 0),
		},
	}
}

func (b *FSReplicationBuilder) AddStep(from, to *FilesystemVersion) *FSReplication {
	step := &FSReplicationStep{
		state: StepPending,
		fsrep: b.r,
		from: from,
		to: to,
	}
	b.r.pending = append(b.r.pending, step)
	return b.r
}

func (b *FSReplicationBuilder) Complete() *FSReplication {
	if len(b.r.pending) > 0 {
		b.r.state = FSQueued
	} else {
		b.r.state = FSCompleted
	}
	r := b.r
	return r
}

//go:generate stringer -type=FSReplicationStepState
type FSReplicationStepState int

const (
	StepPending FSReplicationStepState = iota
	StepActive
	StepRetry
	StepPermanentError
	StepCompleted
)

type FSReplicationStep struct {
	state    FSReplicationStepState
	from, to *FilesystemVersion
	fsrep    *FSReplication

	// both retry and permanent error
	err error
}

func (r *Replication) Drive(ctx context.Context, ep EndpointPair, retryNow chan struct{}) {
	for !(r.state == Completed || r.state == ContextDone) {
		pre := r.state
		preTime := time.Now()
		r.doDrive(ctx, ep, retryNow)
		delta := time.Now().Sub(preTime)
		post := r.state
		getLogger(ctx).
			WithField("transition", fmt.Sprintf("%s => %s", pre, post)).
			WithField("duration", delta).
			Debug("state transition")
	}
}

func (r *Replication) doDrive(ctx context.Context, ep EndpointPair, retryNow chan struct{}) {

	switch r.state {

	case Planning:
		r.tryBuildPlan(ctx, ep)

	case PlanningError:
		w := time.NewTimer(10 * time.Second) // FIXME constant make configurable
		defer w.Stop()
		select {
		case <-ctx.Done():
			r.state = ContextDone
			r.contextError = ctx.Err()
		case <-retryNow:
			r.state = Planning
			r.planningError = nil
		case <-w.C:
			r.state = Planning
			r.planningError = nil
		}

	case Working:

		if len(r.pending) == 0 {
			r.state = Completed
			return
		}

		sort.Slice(r.pending, func(i, j int) bool {
			a, b := r.pending[i], r.pending[j]
			statePrio := func(x *FSReplication) int {
				if !(x.state == FSQueued || x.state == FSRetry) {
					panic(x)
				}
				if x.state == FSQueued {
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
			if a.state == FSQueued {
				return a.nextStepDate().Before(b.nextStepDate())
			}
			if a.state == FSRetry {
				return a.retryAt.Before(b.retryAt)
			}
			panic("should not be reached")
		})

		fsrep := r.pending[0]

		if fsrep.state == FSRetry {
			r.state = WorkingWait
			return
		}
		if fsrep.state != FSQueued {
			panic(fsrep)
		}

		fsState := fsrep.takeStep(ctx, ep)
		if fsState&(FSPermanentError|FSCompleted) != 0 {
			r.pending = r.pending[1:]
			r.completed = append(r.completed, fsrep)
		}

	case WorkingWait:
		fsrep := r.pending[0]
		w := time.NewTimer(fsrep.retryAt.Sub(time.Now()))
		defer w.Stop()
		select {
		case <-ctx.Done():
			r.state = ContextDone
			r.contextError = ctx.Err()
		case <-retryNow:
			for _, fsr := range r.pending {
				fsr.retryNow()
			}
			r.state = Working
		case <-w.C:
			fsrep.retryNow() // avoid timer jitter
			r.state = Working
		}
	}
}

func (r *Replication) tryBuildPlan(ctx context.Context, ep EndpointPair) ReplicationState {

	log := getLogger(ctx)

	planningError := func(err error) ReplicationState {
		r.state = PlanningError
		r.planningError = err
		return r.state
	}
	done := func() ReplicationState {
		r.state = Working
		r.planningError = nil
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

	r.pending = make([]*FSReplication, 0, len(sfss))
	r.completed = make([]*FSReplication, 0, len(sfss))
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
			r.completed = append(r.completed, newFSReplicationPermanentError(fs, err))
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
			r.completed = append(r.completed, newFSReplicationPermanentError(fs, conflict))
			continue
		}

		fsreplbuilder := buildNewFSReplication(fs)
		if len(path) == 1 {
			fsreplbuilder.AddStep(nil, path[0])
		} else {
			for i := 0; i < len(path)-1; i++ {
				fsreplbuilder.AddStep(path[i], path[i+1])
			}
		}
		fsrepl := fsreplbuilder.Complete()
		switch fsrepl.state {
		case FSCompleted:
			r.completed = append(r.completed, fsreplbuilder.Complete())
		case FSQueued:
			r.pending = append(r.pending, fsreplbuilder.Complete())
		default:
			panic(fsrepl)
		}

	}

	return done()
}

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

func (f *FSReplication) takeStep(ctx context.Context, ep EndpointPair) FSReplicationState {
	if f.state != FSQueued {
		panic(f)
	}

	f.state = FSActive
	step := f.pending[0]
	stepState := step.do(ctx, ep)

	switch stepState {
	case StepCompleted:
		f.pending = f.pending[1:]
		f.completed = append(f.completed, step)
		if len(f.pending) > 0 {
			f.state = FSQueued
		} else {
			f.state = FSCompleted
		}

	case StepRetry:
		f.state = FSRetry
		f.retryAt = time.Now().Add(10 * time.Second) // FIXME hardcoded constant

	case StepPermanentError:
		f.state = FSPermanentError

	}
	return f.state
}

func (f *FSReplication) retryNow() {
	if f.state != FSRetry {
		panic(f)
	}
	f.retryAt = time.Time{}
	f.state = FSQueued
}

func (s *FSReplicationStep) do(ctx context.Context, ep EndpointPair) FSReplicationStepState {

	fs := s.fsrep.fs

	log := getLogger(ctx).
		WithField("filesystem", fs.Path).
		WithField("step", s.String())

	updateStateError := func(err error) FSReplicationStepState {
		s.err = err
		switch err {
			case io.EOF: fallthrough
			case io.ErrUnexpectedEOF: fallthrough
			case io.ErrClosedPipe:
				return StepRetry
		}
		if _, ok := err.(net.Error); ok {
			return StepRetry
		}
		return StepPermanentError
	}

	updateStateCompleted := func() FSReplicationStepState {
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

