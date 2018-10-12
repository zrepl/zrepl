// Package fsrep implements replication of a single file system with existing versions
// from a sender to a receiver.
package fsrep

import (
	"context"
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"io"
	"net"
	"sync"
	"time"

	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/replication/pdu"
	"github.com/zrepl/zrepl/util"
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

// A Sender is usually part of a github.com/zrepl/zrepl/replication.Endpoint.
type Sender interface {
	// If a non-nil io.ReadCloser is returned, it is guaranteed to be closed before
	// any next call to the parent github.com/zrepl/zrepl/replication.Endpoint.
	// If the send request is for dry run the io.ReadCloser will be nil
	Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, io.ReadCloser, error)
	ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error)
}

// A Sender is usually part of a github.com/zrepl/zrepl/replication.Endpoint.
type Receiver interface {
	// Receive sends r and sendStream (the latter containing a ZFS send stream)
	// to the parent github.com/zrepl/zrepl/replication.Endpoint.
	// Implementors must guarantee that Close was called on sendStream before
	// the call to Receive returns.
	Receive(ctx context.Context, r *pdu.ReceiveReq, sendStream io.ReadCloser) error
}

type StepReport struct {
	From, To string
	Status   StepState
	Problem  string
	Bytes    int64
	ExpectedBytes int64 // 0 means no size estimate possible
}

type Report struct {
	Filesystem         string
	Status             string
	Problem            string
	Completed, Pending []*StepReport
}

//go:generate stringer -type=State
type State uint

const (
	Ready State = 1 << iota
	RetryWait
	PermanentError
	Completed
)

func (s State) fsrsf() state {
	m := map[State]state{
		Ready:          stateReady,
		RetryWait:      stateRetryWait,
		PermanentError: nil,
		Completed:      nil,
	}
	return m[s]
}

type Replication struct {
	promBytesReplicated prometheus.Counter

	// lock protects all fields below it in this struct, but not the data behind pointers
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

func (f *Replication) UpdateSizeEsitmate(ctx context.Context, sender Sender) error {
	f.lock.Lock()
	defer f.lock.Unlock()
	for _, e := range f.pending {
		if err := e.updateSizeEstimate(ctx, sender); err != nil {
			return err
		}
	}
	return nil
}

type ReplicationBuilder struct {
	r *Replication
}

func BuildReplication(fs string, promBytesReplicated prometheus.Counter) *ReplicationBuilder {
	return &ReplicationBuilder{&Replication{fs: fs, promBytesReplicated: promBytesReplicated}}
}

func (b *ReplicationBuilder) AddStep(from, to FilesystemVersion) *ReplicationBuilder {
	step := &ReplicationStep{
		state:  StepReplicationReady,
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
	r = b.r
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
	StepReplicationReady StepState = 1 << iota
	StepReplicationRetry
	StepMarkReplicatedReady
	StepMarkReplicatedRetry
	StepPermanentError
	StepCompleted
)

type FilesystemVersion interface {
	SnapshotTime() time.Time
	GetName() string // name without @ or #
	RelName() string // name with @ or #
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

	byteCounter  *util.ByteCounterReader
	expectedSize int64 // 0 means no size estimate present / possible
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

var RetrySleepDuration = 10 * time.Second // FIXME make configurable

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

	stepState := current.doReplication(ctx, sender, receiver)

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
		case StepReplicationRetry:
			fallthrough
		case StepMarkReplicatedRetry:
			f.retryWaitUntil = time.Now().Add(RetrySleepDuration)
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

	if fsr.state&RetryWait != 0 {
		if len(rep.Pending) != 0 { // should always be true for RetryWait == true?
			rep.Problem = rep.Pending[0].Problem
		}
	}

	return &rep
}

func shouldRetry(err error) bool {
	switch err {
	case io.EOF:
		fallthrough
	case io.ErrUnexpectedEOF:
		fallthrough
	case io.ErrClosedPipe:
		return true
	}
	if _, ok := err.(net.Error); ok {
		return true
	}
	return false
}

func (s *ReplicationStep) doReplication(ctx context.Context, sender Sender, receiver Receiver) StepState {

	fs := s.parent.fs

	log := getLogger(ctx).
		WithField("filesystem", fs).
		WithField("step", s.String())

	updateStateError := func(err error) StepState {
		s.lock.Lock()
		defer s.lock.Unlock()

		s.err = err
		if shouldRetry(s.err) {
			s.state = StepReplicationRetry
			return s.state
		}
		s.state = StepPermanentError
		return s.state
	}

	updateStateCompleted := func() StepState {
		s.lock.Lock()
		defer s.lock.Unlock()
		s.err = nil
		s.state = StepMarkReplicatedReady
		return s.state
	}

	sr := s.buildSendRequest(false)

	log.Debug("initiate send request")
	sres, sstream, err := sender.Send(ctx, sr)
	if err != nil {
		log.WithError(err).Error("send request failed")
		return updateStateError(err)
	}
	if sstream == nil {
		err := errors.New("send request did not return a stream, broken endpoint implementation")
		return updateStateError(err)
	}

	s.byteCounter = util.NewByteCounterReader(sstream)
	defer func() {
		s.parent.promBytesReplicated.Add(float64(s.byteCounter.Bytes()))
	}()
	sstream = s.byteCounter

	rr := &pdu.ReceiveReq{
		Filesystem:       fs,
		ClearResumeToken: !sres.UsedResumeToken,
	}
	log.Debug("initiate receive request")
	err = receiver.Receive(ctx, rr, sstream)
	if err != nil {
		log.
			WithError(err).
			WithField("errType", fmt.Sprintf("%T", err)).
			Error("receive request failed (might also be error on sender)")
		sstream.Close()
		// This failure could be due to
		// 	- an unexpected exit of ZFS on the sending side
		//  - an unexpected exit of ZFS on the receiving side
		//  - a connectivity issue
		return updateStateError(err)
	}
	log.Debug("receive finished")

	updateStateCompleted()

	return s.doMarkReplicated(ctx, sender)

}

func (s *ReplicationStep) doMarkReplicated(ctx context.Context, sender Sender) StepState {

	log := getLogger(ctx).
		WithField("filesystem", s.parent.fs).
		WithField("step", s.String())

	updateStateError := func(err error) StepState {
		s.lock.Lock()
		defer s.lock.Unlock()

		s.err = err
		if shouldRetry(s.err) {
			s.state = StepMarkReplicatedRetry
			return s.state
		}
		s.state = StepPermanentError
		return s.state
	}

	updateStateCompleted := func() StepState {
		s.lock.Lock()
		defer s.lock.Unlock()
		s.state = StepCompleted
		return s.state
	}

	log.Debug("advance replication cursor")
	req := &pdu.ReplicationCursorReq{
		Filesystem: s.parent.fs,
		Op: &pdu.ReplicationCursorReq_Set{
			Set: &pdu.ReplicationCursorReq_SetOp{
				Snapshot:   s.to.GetName(),
			},
		},
	}
	res, err := sender.ReplicationCursor(ctx, req)
	if err != nil {
		log.WithError(err).Error("error advancing replication cursor")
		return updateStateError(err)
	}
	if res.GetError() != "" {
		err := fmt.Errorf("cannot advance replication cursor: %s", res.GetError())
		log.Error(err.Error())
		return updateStateError(err)
	}

	return updateStateCompleted()
}

func (s *ReplicationStep) updateSizeEstimate(ctx context.Context, sender Sender) error {

	fs := s.parent.fs

	log := getLogger(ctx).
		WithField("filesystem", fs).
		WithField("step", s.String())

	sr := s.buildSendRequest(true)

	log.Debug("initiate dry run send request")
	sres, _, err := sender.Send(ctx, sr)
	if err != nil {
		log.WithError(err).Error("dry run send request failed")
		return err
	}
	s.expectedSize = sres.ExpectedSize
	return nil
}

func (s *ReplicationStep) buildSendRequest(dryRun bool) (sr *pdu.SendReq) {
	fs := s.parent.fs
	if s.from == nil {
		sr = &pdu.SendReq{
			Filesystem: fs,
			From:       s.to.RelName(), // FIXME fix protocol to use To, like zfs does internally
			DryRun:     dryRun,
		}
	} else {
		sr = &pdu.SendReq{
			Filesystem: fs,
			From:       s.from.RelName(),
			To:         s.to.RelName(),
			DryRun:     dryRun,
		}
	}
	return sr
}

func (s *ReplicationStep) String() string {
	if s.from == nil { // FIXME: ZFS semantics are that to is nil on non-incremental send
		return fmt.Sprintf("%s%s (full)", s.parent.fs, s.to.RelName())
	} else {
		return fmt.Sprintf("%s(%s => %s)", s.parent.fs, s.from.RelName(), s.to.RelName())
	}
}

func (s *ReplicationStep) Report() *StepReport {
	var from string // FIXME follow same convention as ZFS: to should be nil on full send
	if s.from != nil {
		from = s.from.RelName()
	}
	bytes := int64(0)
	if s.byteCounter != nil {
		bytes = s.byteCounter.Bytes()
	}
	problem := ""
	if s.err != nil {
		problem = s.err.Error()
	}
	rep := StepReport{
		From:   from,
		To:     s.to.RelName(),
		Status: s.state,
		Problem: problem,
		Bytes:  bytes,
		ExpectedBytes: s.expectedSize,
	}
	return &rep
}
