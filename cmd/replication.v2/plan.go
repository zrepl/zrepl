package replication

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net"
	"sort"
	"sync"
	"time"
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

type replicationQueueItem struct {
	retriesSinceLastError int
	// duplicates fsr.state to avoid accessing and locking fsr
	state FSReplicationState
	// duplicates fsr.current.nextStepDate to avoid accessing & locking fsr
	nextStepDate time.Time

	fsr *FSReplication
}

type Replication struct {
	// lock protects all fields of this struct (but not the fields behind pointers!)
	lock sync.Mutex

	state ReplicationState

	// Working, WorkingWait, Completed, ContextDone
	queue     *replicationQueue
	completed []*FSReplication
	active    *FSReplication

	// PlanningError
	planningError error

	// ContextDone
	contextError error

	// PlanningError, WorkingWait
	sleepUntil time.Time
}

type replicationUpdater func(func(*Replication)) (newState ReplicationState)
type replicationStateFunc func(context.Context, EndpointPair, replicationUpdater) replicationStateFunc

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
	fs                 *Filesystem
	err                error
	retryWaitUntil     time.Time
	completed, pending []*FSReplicationStep
	current            *FSReplicationStep
}

func newReplicationQueueItemPermanentError(fs *Filesystem, err error) *replicationQueueItem {
	return &replicationQueueItem{
		retriesSinceLastError: 0,
		state: FSPermanentError,
		fsr: &FSReplication{
			state: FSPermanentError,
			fs:    fs,
			err:   err,
		},
	}
}

func (b *replicationQueueItemBuilder) Complete() *replicationQueueItem {
	if len(b.r.pending) > 0 {
		b.r.state = FSReady
	} else {
		b.r.state = FSCompleted
	}
	r := b.r
	return &replicationQueueItem{0, b.r.state, time.Time{}, r}
}

//go:generate stringer -type=FSReplicationStepState
type FSReplicationStepState uint

const (
	StepReady FSReplicationStepState = 1 << iota
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
		getLogger(ctx).
			WithField("transition", fmt.Sprintf("%s => %s", pre, post)).
			WithField("duration", delta).
			Debug("main state transition")
	}

	getLogger(ctx).
		WithField("final_state", post).
		Debug("main final state")
}

func rsfPlanning(ctx context.Context, ep EndpointPair, u replicationUpdater) replicationStateFunc {

	log := getLogger(ctx)

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

	q := newReplicationQueue()
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
			q.Add(newReplicationQueueItemPermanentError(fs, err))
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
				return handlePlanningError(err)
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
			q.Add(newReplicationQueueItemPermanentError(fs, conflict))
			continue
		}

		builder := buildReplicationQueueItem(fs)
		if len(path) == 1 {
			builder.AddStep(nil, path[0])
		} else {
			for i := 0; i < len(path)-1; i++ {
				builder.AddStep(path[i], path[i+1])
			}
		}
		qitem := builder.Complete()
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

	var active *replicationQueueItemHandle

	rsfNext := u(func(r *Replication) {
		done, next := r.queue.GetNext()
		r.completed = append(r.completed, done...)
		if next == nil {
			r.state = Completed
		}
		active = next
	}).rsf()

	if active == nil {
		return rsfNext
	}

	state, nextStepDate := active.GetFSReplication().takeStep(ctx, ep)

	return u(func(r *Replication) {
		active.Update(state, nextStepDate)
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

type replicationQueueItemBuilder struct {
	r     *FSReplication
	steps []*FSReplicationStep
}

func buildReplicationQueueItem(fs *Filesystem) *replicationQueueItemBuilder {
	return &replicationQueueItemBuilder{
		r: &FSReplication{
			fs:      fs,
			pending: make([]*FSReplicationStep, 0),
		},
	}
}

func (b *replicationQueueItemBuilder) AddStep(from, to *FilesystemVersion) *replicationQueueItemBuilder {
	step := &FSReplicationStep{
		state: StepReady,
		fsrep: b.r,
		from:  from,
		to:    to,
	}
	b.r.pending = append(b.r.pending, step)
	return b
}

type replicationQueue []*replicationQueueItem

func newReplicationQueue() *replicationQueue {
	q := make(replicationQueue, 0)
	return &q
}

func (q replicationQueue) Len() int      { return len(q) }
func (q replicationQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q replicationQueue) Less(i, j int) bool {
	a, b := q[i], q[j]
	statePrio := func(x *replicationQueueItem) int {
		if x.state&(FSReady|FSRetryWait) == 0 {
			panic(x)
		}
		if x.state == FSReady {
			return 0
		}
		return 1
	}

	aprio, bprio := statePrio(a), statePrio(b)
	if aprio != bprio {
		return aprio < bprio
	}
	// now we know they are the same state
	if a.state == FSReady {
		return a.nextStepDate.Before(b.nextStepDate)
	}
	if a.state == FSRetryWait {
		return a.retriesSinceLastError < b.retriesSinceLastError
	}
	panic("should not be reached")
}

func (q *replicationQueue) sort() (done []*FSReplication) {
	// pre-scan for everything that is not ready
	newq := make(replicationQueue, 0, len(*q))
	done = make([]*FSReplication, 0, len(*q))
	for _, qitem := range *q {
		if qitem.state&(FSReady|FSRetryWait) == 0 {
			done = append(done, qitem.fsr)
		} else {
			newq = append(newq, qitem)
		}
	}
	sort.SortStable(newq) // stable to avoid flickering in reports
	*q = newq
	return done
}

// next remains valid until the next call to GetNext()
func (q *replicationQueue) GetNext() (done []*FSReplication, next *replicationQueueItemHandle) {
	done = q.sort()
	if len(*q) == 0 {
		return done, nil
	}
	next = &replicationQueueItemHandle{(*q)[0]}
	return done, next
}

func (q *replicationQueue) Add(qitem *replicationQueueItem) {
	*q = append(*q, qitem)
}

func (q *replicationQueue) Foreach(fu func(*replicationQueueItemHandle)) {
	for _, qitem := range *q {
		fu(&replicationQueueItemHandle{qitem})
	}
}

type replicationQueueItemHandle struct {
	i *replicationQueueItem
}

func (h replicationQueueItemHandle) GetFSReplication() *FSReplication {
	return h.i.fsr
}

func (h replicationQueueItemHandle) Update(newState FSReplicationState, nextStepDate time.Time) {
	h.i.state = newState
	h.i.nextStepDate = nextStepDate
	if h.i.state&FSReady != 0 {
		h.i.retriesSinceLastError = 0
	} else if h.i.state&FSRetryWait != 0 {
		h.i.retriesSinceLastError++
	}
}

func (f *FSReplication) takeStep(ctx context.Context, ep EndpointPair) (post FSReplicationState, nextStepDate time.Time) {

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
	s = s(ctx, ep, u)
	delta := time.Now().Sub(preTime)
	post = u(func(f *FSReplication) {
		if f.state != FSReady {
			return
		}
		ct, err := f.current.to.CreationAsTime()
		if err != nil {
			panic(err) // FIXME
		}
		nextStepDate = ct
	})

	getLogger(ctx).
		WithField("fs", f.fs.Path).
		WithField("transition", fmt.Sprintf("%s => %s", pre, post)).
		WithField("duration", delta).
		Debug("fsr step taken")

	return post, nextStepDate
}

type fsrUpdater func(func(fsr *FSReplication)) FSReplicationState
type fsrsf func(ctx context.Context, ep EndpointPair, u fsrUpdater) fsrsf

func fsrsfReady(ctx context.Context, ep EndpointPair, u fsrUpdater) fsrsf {

	var current *FSReplicationStep
	s := u(func(f *FSReplication) {
		if f.current == nil {
			if len(f.pending) == 0 {
				f.state = FSCompleted
				return
			}
			f.current = f.pending[0]
			f.pending = f.pending[1:]
		}
		current = f.current
	})
	if s != FSReady {
		return s.fsrsf()
	}

	stepState := current.do(ctx, ep)

	return u(func(f *FSReplication) {
		switch stepState {
		case StepCompleted:
			f.completed = append(f.completed, f.current)
			f.current = nil
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

func fsrsfRetryWait(ctx context.Context, ep EndpointPair, u fsrUpdater) fsrsf {
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
