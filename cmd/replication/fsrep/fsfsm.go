// Package fsrep implements replication of a single file system with existing versions
// from a sender to a receiver.
package fsrep

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
	"github.com/zrepl/zrepl/logger"
)

type contextKey int

const (
	contextKeyLogger contextKey = iota
)

type Logger = logger.Logger

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLogger, log)
}

func getLogger(ctx context.Context) Logger {
	l, ok := ctx.Value(contextKeyLogger).(Logger)
	if !ok {
		l = logger.NewNullLogger()
	}
	return l
}

type Sender interface {
	Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, io.ReadCloser, error)
}

type Receiver interface {
	Receive(ctx context.Context, r *pdu.ReceiveReq, sendStream io.ReadCloser) error
}

type StepReport struct {
	From, To string
	Status   string
	Problem  string
}

type Report struct {
	Filesystem string
	Status     string
	Problem    string
	Completed,Pending  []*StepReport
}


//go:generate stringer -type=State
type State uint

const (
	Ready          State = 1 << iota
	RetryWait
	PermanentError
	Completed
)

func (s State) fsrsf() state {
	idx := bits.TrailingZeros(uint(s))
	if idx == bits.UintSize {
		panic(s)
	}
	m := []state{
		stateReady,
		stateRetryWait,
		nil,
		nil,
	}
	return m[idx]
}

type Replication struct {
	// lock protects all fields in this struct, but not the data behind pointers
	lock               sync.Mutex
	state              State
	fs                 string
	err                error
	retryWaitUntil     time.Time
	completed, pending []*ReplicationStep
}

func (f *Replication) State() State {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.state
}

type ReplicationBuilder struct {
	r *Replication
}

func BuildReplication(fs string) *ReplicationBuilder {
	return &ReplicationBuilder{&Replication{fs: fs}}
}

func (b *ReplicationBuilder) AddStep(from, to FilesystemVersion) *ReplicationBuilder {
	step := &ReplicationStep{
		state:  StepReady,
		parent: b.r,
		from:   from,
		to:     to,
	}
	b.r.pending = append(b.r.pending, step)
	return b
}

func (b *ReplicationBuilder) Done() (r *Replication) {
	if len(b.r.pending) > 0 {
		b.r.state = Ready
	} else {
		b.r.state = Completed
	}
	r  = b.r
	b.r = nil
	return r
}

func NewReplicationWithPermanentError(fs string, err error) *Replication {
	return &Replication{
		state: PermanentError,
		fs:    fs,
		err:   err,
	}
}


//go:generate stringer -type=StepState
type StepState uint

const (
	StepReady          StepState = 1 << iota
	StepRetry
	StepPermanentError
	StepCompleted
)

type FilesystemVersion interface {
	SnapshotTime() time.Time
	RelName() string
}

type ReplicationStep struct {
	// only protects state, err
	// from, to and parent are assumed to be immutable
	lock sync.Mutex

	state    StepState
	from, to FilesystemVersion
	parent   *Replication

	// both retry and permanent error
	err error
}

func (f *Replication) TakeStep(ctx context.Context, sender Sender, receiver Receiver) (post State, nextStepDate time.Time) {

	var u updater = func(fu func(*Replication)) State {
		f.lock.Lock()
		defer f.lock.Unlock()
		if fu != nil {
			fu(f)
		}
		return f.state
	}
	var s state = u(nil).fsrsf()

	pre := u(nil)
	preTime := time.Now()
	s = s(ctx, sender, receiver, u)
	delta := time.Now().Sub(preTime)
	post = u(func(f *Replication) {
		if len(f.pending) == 0 {
			return
		}
		nextStepDate = f.pending[0].to.SnapshotTime()
	})

	getLogger(ctx).
		WithField("fs", f.fs).
		WithField("transition", fmt.Sprintf("%s => %s", pre, post)).
		WithField("duration", delta).
		Debug("fsr step taken")

	return post, nextStepDate
}

type updater func(func(fsr *Replication)) State

type state func(ctx context.Context, sender Sender, receiver Receiver, u updater) state

func stateReady(ctx context.Context, sender Sender, receiver Receiver, u updater) state {

	var current *ReplicationStep
	s := u(func(f *Replication) {
		if len(f.pending) == 0 {
			f.state = Completed
			return
		}
		current = f.pending[0]
	})
	if s != Ready {
		return s.fsrsf()
	}

	stepState := current.do(ctx, sender, receiver)

	return u(func(f *Replication) {
		switch stepState {
		case StepCompleted:
			f.completed = append(f.completed, current)
			f.pending = f.pending[1:]
			if len(f.pending) > 0 {
				f.state = Ready
			} else {
				f.state = Completed
			}
		case StepRetry:
			f.retryWaitUntil = time.Now().Add(10 * time.Second) // FIXME make configurable
			f.state = RetryWait
		case StepPermanentError:
			f.state = PermanentError
			f.err = errors.New("a replication step failed with a permanent error")
		default:
			panic(f)
		}
	}).fsrsf()
}

func stateRetryWait(ctx context.Context, sender Sender, receiver Receiver, u updater) state {
	var sleepUntil time.Time
	u(func(f *Replication) {
		sleepUntil = f.retryWaitUntil
	})
	t := time.NewTimer(sleepUntil.Sub(time.Now()))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return u(func(f *Replication) {
			f.state = PermanentError
			f.err = ctx.Err()
		}).fsrsf()
	case <-t.C:
	}
	return u(func(f *Replication) {
		f.state = Ready
	}).fsrsf()
}

func (fsr *Replication) Report() *Report {
	fsr.lock.Lock()
	defer fsr.lock.Unlock()

	rep := Report{
		Filesystem: fsr.fs,
		Status:     fsr.state.String(),
	}

	if fsr.state&PermanentError != 0 {
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

func (s *ReplicationStep) do(ctx context.Context, sender Sender, receiver Receiver) StepState {

	fs := s.parent.fs

	log := getLogger(ctx).
		WithField("filesystem", fs).
		WithField("step", s.String())

	updateStateError := func(err error) StepState {
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

	updateStateCompleted := func() StepState {
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

func (s *ReplicationStep) String() string {
	if s.from == nil { // FIXME: ZFS semantics are that to is nil on non-incremental send
		return fmt.Sprintf("%s%s (full)", s.parent.fs, s.to.RelName())
	} else {
		return fmt.Sprintf("%s(%s => %s)", s.parent.fs, s.from, s.to.RelName())
	}
}

func (step *ReplicationStep) Report() *StepReport {
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

