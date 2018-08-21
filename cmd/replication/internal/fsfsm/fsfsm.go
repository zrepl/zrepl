package fsfsm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net"
	"sync"
	"time"

	"github.com/zrepl/zrepl/cmd/replication/pdu"
	. "github.com/zrepl/zrepl/cmd/replication/common"
)

type StepReport struct {
	From, To string
	Status   string
	Problem  string
}

type FilesystemReplicationReport struct {
	Filesystem string
	Status     string
	Problem    string
	Completed,Pending  []*StepReport
}


//go:generate stringer -type=FSReplicationState
type FSReplicationState uint

const (
	FSReady FSReplicationState = 1 << iota
	FSRetryWait
	FSPermanentError
	FSCompleted
)

func (s FSReplicationState) fsrsf() fsrsf {
	idx := bits.TrailingZeros(uint(s))
	if idx == bits.UintSize {
		panic(s)
	}
	m := []fsrsf{
		fsrsfReady,
		fsrsfRetryWait,
		nil,
		nil,
	}
	return m[idx]
}

type FSReplication struct {
	// lock protects all fields in this struct, but not the data behind pointers
	lock               sync.Mutex
	state              FSReplicationState
	fs                 string
	err                error
	retryWaitUntil     time.Time
	completed, pending []*FSReplicationStep
}

func (f *FSReplication) State() FSReplicationState {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.state
}

type FSReplicationBuilder struct {
	r *FSReplication
}

func BuildFSReplication(fs string) *FSReplicationBuilder {
	return &FSReplicationBuilder{&FSReplication{fs: fs}}
}

func (b *FSReplicationBuilder) AddStep(from, to FilesystemVersion) *FSReplicationBuilder {
	step := &FSReplicationStep{
		state: StepReady,
		fsrep: b.r,
		from:  from,
		to:    to,
	}
	b.r.pending = append(b.r.pending, step)
	return b
}

func (b *FSReplicationBuilder) Done() (r *FSReplication) {
	if len(b.r.pending) > 0 {
		b.r.state = FSReady
	} else {
		b.r.state = FSCompleted
	}
	r  = b.r
	b.r = nil
	return r
}

func NewFSReplicationWithPermanentError(fs string, err error) *FSReplication {
	return &FSReplication{
		state: FSPermanentError,
		fs:    fs,
		err:   err,
	}
}


//go:generate stringer -type=FSReplicationStepState
type FSReplicationStepState uint

const (
	StepReady FSReplicationStepState = 1 << iota
	StepRetry
	StepPermanentError
	StepCompleted
)

type FilesystemVersion interface {
	SnapshotTime() time.Time
	RelName() string
}

type FSReplicationStep struct {
	// only protects state, err
	// from, to and fsrep are assumed to be immutable
	lock sync.Mutex

	state    FSReplicationStepState
	from, to FilesystemVersion
	fsrep    *FSReplication

	// both retry and permanent error
	err error
}

func (f *FSReplication) TakeStep(ctx context.Context, sender, receiver ReplicationEndpoint) (post FSReplicationState, nextStepDate time.Time) {

	var u fsrUpdater = func(fu func(*FSReplication)) FSReplicationState {
		f.lock.Lock()
		defer f.lock.Unlock()
		if fu != nil {
			fu(f)
		}
		return f.state
	}
	var s fsrsf = u(nil).fsrsf()

	pre := u(nil)
	preTime := time.Now()
	s = s(ctx, sender, receiver, u)
	delta := time.Now().Sub(preTime)
	post = u(func(f *FSReplication) {
		if len(f.pending) == 0 {
			return
		}
		nextStepDate = f.pending[0].to.SnapshotTime()
	})

	GetLogger(ctx).
		WithField("fs", f.fs).
		WithField("transition", fmt.Sprintf("%s => %s", pre, post)).
		WithField("duration", delta).
		Debug("fsr step taken")

	return post, nextStepDate
}

type fsrUpdater func(func(fsr *FSReplication)) FSReplicationState

type fsrsf func(ctx context.Context, sender, receiver ReplicationEndpoint, u fsrUpdater) fsrsf

func fsrsfReady(ctx context.Context, sender, receiver ReplicationEndpoint, u fsrUpdater) fsrsf {

	var current *FSReplicationStep
	s := u(func(f *FSReplication) {
		if len(f.pending) == 0 {
			f.state = FSCompleted
			return
		}
		current = f.pending[0]
	})
	if s != FSReady {
		return s.fsrsf()
	}

	stepState := current.do(ctx, sender, receiver)

	return u(func(f *FSReplication) {
		switch stepState {
		case StepCompleted:
			f.completed = append(f.completed, current)
			f.pending = f.pending[1:]
			if len(f.pending) > 0 {
				f.state = FSReady
			} else {
				f.state = FSCompleted
			}
		case StepRetry:
			f.retryWaitUntil = time.Now().Add(10 * time.Second) // FIXME make configurable
			f.state = FSRetryWait
		case StepPermanentError:
			f.state = FSPermanentError
			f.err = errors.New("a replication step failed with a permanent error")
		default:
			panic(f)
		}
	}).fsrsf()
}

func fsrsfRetryWait(ctx context.Context, sender, receiver ReplicationEndpoint, u fsrUpdater) fsrsf {
	var sleepUntil time.Time
	u(func(f *FSReplication) {
		sleepUntil = f.retryWaitUntil
	})
	t := time.NewTimer(sleepUntil.Sub(time.Now()))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return u(func(f *FSReplication) {
			f.state = FSPermanentError
			f.err = ctx.Err()
		}).fsrsf()
	case <-t.C:
	}
	return u(func(f *FSReplication) {
		f.state = FSReady
	}).fsrsf()
}

// access to fsr's members must be exclusive
func (fsr *FSReplication) Report() *FilesystemReplicationReport {
	fsr.lock.Lock()
	defer fsr.lock.Unlock()

	rep := FilesystemReplicationReport{
		Filesystem: fsr.fs,
		Status:     fsr.state.String(),
	}

	if fsr.state&FSPermanentError != 0 {
		rep.Problem = fsr.err.Error()
		return &rep
	}

	rep.Completed = make([]*StepReport, len(fsr.completed))
	for i := range fsr.completed {
		rep.Completed[i] = fsr.completed[i].Report()
	}
	rep.Pending = make([]*StepReport, len(fsr.pending))
	for i := range fsr.pending {
		rep.Pending[i] = fsr.pending[i].Report()
	}
	return &rep
}

func (s *FSReplicationStep) do(ctx context.Context, sender, receiver ReplicationEndpoint) FSReplicationStepState {

	fs := s.fsrep.fs

	log := GetLogger(ctx).
		WithField("filesystem", fs).
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

	var sr *pdu.SendReq
	if s.from == nil {
		sr = &pdu.SendReq{
			Filesystem: fs,
			From:       s.to.RelName(), // FIXME fix protocol to use To, like zfs does internally
		}
	} else {
		sr = &pdu.SendReq{
			Filesystem: fs,
			From:       s.from.RelName(),
			To:         s.to.RelName(),
		}
	}

	log.WithField("request", sr).Debug("initiate send request")
	sres, sstream, err := sender.Send(ctx, sr)
	if err != nil {
		log.WithError(err).Error("send request failed")
		return updateStateError(err)
	}
	if sstream == nil {
		err := errors.New("send request did not return a stream, broken endpoint implementation")
		return updateStateError(err)
	}

	rr := &pdu.ReceiveReq{
		Filesystem:       fs,
		ClearResumeToken: !sres.UsedResumeToken,
	}
	log.WithField("request", rr).Debug("initiate receive request")
	err = receiver.Receive(ctx, rr, sstream)
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
		return fmt.Sprintf("%s%s (full)", s.fsrep.fs, s.to.RelName())
	} else {
		return fmt.Sprintf("%s(%s => %s)", s.fsrep.fs, s.from, s.to.RelName())
	}
}

func (step *FSReplicationStep) Report() *StepReport {
	var from string // FIXME follow same convention as ZFS: to should be nil on full send
	if step.from != nil {
		from = step.from.RelName()
	}
	rep := StepReport{
		From:   from,
		To:     step.to.RelName(),
		Status: step.state.String(),
	}
	return &rep
}

