// Package fsrep implements replication of a single file system with existing versions
// from a sender to a receiver.
package fsrep

import (
	"context"
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/util/watchdog"
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

//go:generate enumer -type=State
type State uint

const (
	Ready State = 1 << iota
	Completed
)

type Error interface {
	error
	Temporary() bool
	LocalToFS() bool
}

type Replication struct {
	promBytesReplicated prometheus.Counter

	fs                 string

	// lock protects all fields below it in this struct, but not the data behind pointers
	lock               sync.Mutex
	state              State
	err                Error
	completed, pending []*ReplicationStep
}

func (f *Replication) State() State {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.state
}

func (f *Replication) FS() string { return f.fs }

// returns zero value time.Time{} if no more pending steps
func (f *Replication) NextStepDate() time.Time {
	if len(f.pending) == 0 {
		return time.Time{}
	}
	return f.pending[0].to.SnapshotTime()
}

func (f *Replication) Err() Error {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.err
}

func (f *Replication) CanRetry() bool {
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.state == Completed {
		return false
	}
	if f.state != Ready {
		panic(fmt.Sprintf("implementation error: %v", f.state))
	}
	if f.err == nil {
		return true
	}
	return f.err.Temporary()
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

type ReplicationConflictError struct {
	Err error
}

func (e *ReplicationConflictError) Timeout() bool { return false }

func (e *ReplicationConflictError) Temporary() bool { return false }

func (e *ReplicationConflictError) Error() string { return fmt.Sprintf("permanent error: %s", e.Err.Error()) }

func (e *ReplicationConflictError) LocalToFS() bool { return true }

func NewReplicationConflictError(fs string, err error) *Replication {
	return &Replication{
		state: Completed,
		fs:    fs,
		err:   &ReplicationConflictError{Err: err},
	}
}

//go:generate enumer -type=StepState
type StepState uint

const (
	StepReplicationReady StepState = 1 << iota
	StepMarkReplicatedReady
	StepCompleted
)

func (s StepState) IsTerminal() bool { return s == StepCompleted }

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

func (f *Replication) Retry(ctx context.Context, ka *watchdog.KeepAlive, sender Sender, receiver Receiver) Error {

	var u updater = func(fu func(*Replication)) State {
		f.lock.Lock()
		defer f.lock.Unlock()
		if fu != nil {
			fu(f)
		}
		return f.state
	}


	var current *ReplicationStep
	pre := u(nil)
	getLogger(ctx).WithField("fsrep_state", pre).Debug("begin fsrep.Retry")
	defer func() {
		post := u(nil)
		getLogger(ctx).WithField("fsrep_transition", post).Debug("end fsrep.Retry")
	}()

	st := u(func(f *Replication) {
		if len(f.pending) == 0 {
			f.state = Completed
			return
		}
		current = f.pending[0]
	})
	if st == Completed {
		return nil
	}
	if st != Ready {
		panic(fmt.Sprintf("implementation error: %v", st))
	}

	stepCtx := WithLogger(ctx, getLogger(ctx).WithField("step", current))
	getLogger(stepCtx).Debug("take step")
	err := current.Retry(stepCtx, ka, sender, receiver)
	if err != nil {
		getLogger(stepCtx).WithError(err).Error("step could not be completed")
	}

	u(func(fsr *Replication) {
		if err != nil {
			f.err = &StepError{stepStr: current.String(), err: err}
			return
		}
		if err == nil && current.state != StepCompleted {
			panic(fmt.Sprintf("implementation error: %v", current.state))
		}
		f.err = nil
		f.completed = append(f.completed, current)
		f.pending = f.pending[1:]
		if len(f.pending) > 0 {
			f.state = Ready
		} else {
			f.state = Completed
		}
	})
	var retErr Error = nil
	u(func(fsr *Replication) {
		retErr = fsr.err
	})
	return retErr
}

type updater func(func(fsr *Replication)) State

type state func(ctx context.Context, ka *watchdog.KeepAlive, sender Sender, receiver Receiver, u updater) state

type StepError struct {
	stepStr string
	err     error
}

var _ Error = &StepError{}

func (e StepError) Error() string {
	if e.LocalToFS() {
		return fmt.Sprintf("step %s failed: %s", e.stepStr, e.err)
	}
	return e.err.Error()
}

func (e StepError) Timeout() bool {
	if neterr, ok := e.err.(net.Error); ok {
		return neterr.Timeout()
	}
	return false
}

func (e StepError) Temporary() bool {
	if neterr, ok := e.err.(net.Error); ok {
		return neterr.Temporary()
	}
	return false
}

func (e StepError) LocalToFS() bool {
	if _, ok := e.err.(net.Error); ok {
		return false
	}
	return true // conservative approximation: we'd like to check for specific errors returned over RPC here...
}

func (fsr *Replication) Report() *Report {
	fsr.lock.Lock()
	defer fsr.lock.Unlock()

	rep := Report{
		Filesystem: fsr.fs,
		Status:     fsr.state.String(),
	}

	if fsr.err != nil && fsr.err.LocalToFS() {
		rep.Problem = fsr.err.Error()
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

func (s *ReplicationStep) Retry(ctx context.Context, ka *watchdog.KeepAlive, sender Sender, receiver Receiver) error {
	switch s.state {
	case StepReplicationReady:
		return s.doReplication(ctx, ka, sender, receiver)
	case StepMarkReplicatedReady:
		return s.doMarkReplicated(ctx, ka, sender)
	case StepCompleted:
		return nil
	}
	panic(fmt.Sprintf("implementation error: %v", s.state))
}

func (s *ReplicationStep) Error() error {
	if s.state & (StepReplicationReady|StepMarkReplicatedReady) != 0 {
		return s.err
	}
	return nil
}

func (s *ReplicationStep) doReplication(ctx context.Context, ka *watchdog.KeepAlive, sender Sender, receiver Receiver) error {

	if s.state != StepReplicationReady {
		panic(fmt.Sprintf("implementation error: %v", s.state))
	}

	fs := s.parent.fs

	log := getLogger(ctx)
	sr := s.buildSendRequest(false)

	log.Debug("initiate send request")
	sres, sstream, err := sender.Send(ctx, sr)
	if err != nil {
		log.WithError(err).Error("send request failed")
		return err
	}
	if sstream == nil {
		err := errors.New("send request did not return a stream, broken endpoint implementation")
		return err
	}

	s.byteCounter = util.NewByteCounterReader(sstream)
	s.byteCounter.SetCallback(1*time.Second, func(i int64) {
		ka.MadeProgress()
	})
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
		return err
	}
	log.Debug("receive finished")
	ka.MadeProgress()

	s.state = StepMarkReplicatedReady
	return s.doMarkReplicated(ctx, ka, sender)

}

func (s *ReplicationStep) doMarkReplicated(ctx context.Context, ka *watchdog.KeepAlive, sender Sender) error {

	if s.state != StepMarkReplicatedReady {
		panic(fmt.Sprintf("implementation error: %v", s.state))
	}

	log := getLogger(ctx)

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
		return err
	}
	if res.GetError() != "" {
		err := fmt.Errorf("cannot advance replication cursor: %s", res.GetError())
		log.Error(err.Error())
		return err
	}
	ka.MadeProgress()

	s.state = StepCompleted
	return err
}

func (s *ReplicationStep) updateSizeEstimate(ctx context.Context, sender Sender) error {

	log := getLogger(ctx)

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
			To:         s.to.RelName(),
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
