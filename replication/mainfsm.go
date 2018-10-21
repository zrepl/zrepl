// Package replication implements replication of filesystems with existing
// versions (snapshots) from a sender to a receiver.
package replication

import (
	"context"
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/daemon/job/wakeup"
	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/util/watchdog"
	"github.com/problame/go-streamrpc"
	"math/bits"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/zrepl/zrepl/replication/fsrep"
	. "github.com/zrepl/zrepl/replication/internal/diff"
	"github.com/zrepl/zrepl/replication/pdu"
)

//go:generate enumer -type=State
type State uint

const (
	Planning State = 1 << iota
	PlanningError
	Working
	WorkingWait
	Completed
	PermanentError
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

func (s State) IsTerminal() bool {
	return s.rsf() == nil
}

// Replication implements the replication of multiple file systems from a Sender to a Receiver.
//
// It is a state machine that is driven by the Drive method
// and provides asynchronous reporting via the Report method (i.e. from another goroutine).
type Replication struct {
	// not protected by lock
	promSecsPerState *prometheus.HistogramVec // labels: state
	promBytesReplicated *prometheus.CounterVec // labels: filesystem

	Progress watchdog.KeepAlive

	// lock protects all fields of this struct (but not the fields behind pointers!)
	lock sync.Mutex

	state State

	// Working, WorkingWait, Completed, ContextDone
	queue     []*fsrep.Replication
	completed []*fsrep.Replication
	active    *fsrep.Replication // == queue[0] or nil, unlike in Report

	// for PlanningError, WorkingWait and ContextError and Completed
	err error

	// PlanningError, WorkingWait
	sleepUntil time.Time
}

type Report struct {
	Status    string
	Problem   string
	SleepUntil time.Time
	Completed []*fsrep.Report
	Pending   []*fsrep.Report
	Active    *fsrep.Report // not contained in Pending, unlike in struct Replication
}

func NewReplication(secsPerState *prometheus.HistogramVec, bytesReplicated *prometheus.CounterVec) *Replication {
	r := Replication{
		promSecsPerState: secsPerState,
		promBytesReplicated: bytesReplicated,
		state:            Planning,
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
type state func(ctx context.Context, ka *watchdog.KeepAlive, sender Sender, receiver Receiver, u updater) state

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
		s = s(ctx, &r.Progress, sender, receiver, u)
		delta := time.Now().Sub(preTime)
		r.promSecsPerState.WithLabelValues(pre.String()).Observe(delta.Seconds())
		post = u(nil)
		getLogger(ctx).
			WithField("transition", fmt.Sprintf("%s => %s", pre, post)).
			WithField("duration", delta).
			Debug("main state transition")
		if post == Working && pre != post {
			getLogger(ctx).Info("start working")
		}
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

var RetryInterval = envconst.Duration("ZREPL_REPLICATION_RETRY_INTERVAL", 4 * time.Second)

type Error interface {
	error
	Temporary() bool
}

var _ Error = fsrep.Error(nil)
var _ Error = net.Error(nil)
var _ Error = streamrpc.Error(nil)

func isPermanent(err error) bool {
	if e, ok := err.(Error); ok {
		return !e.Temporary()
	}
	return true
}

func statePlanning(ctx context.Context, ka *watchdog.KeepAlive, sender Sender, receiver Receiver, u updater) state {

	log := getLogger(ctx)

	log.Info("start planning")

	handlePlanningError := func(err error) state {
		return u(func(r *Replication) {
			ge := GlobalError{Err: err, Temporary: !isPermanent(err)}
			log.WithError(ge).Error("encountered global error while planning replication")
			r.err = ge
			if !ge.Temporary {
				r.state = PermanentError
			} else {
				r.sleepUntil = time.Now().Add(RetryInterval)
				r.state = PlanningError
			}
		}).rsf()
	}

	sfss, err := sender.ListFilesystems(ctx)
	if err != nil {
		log.WithError(err).Error("error listing sender filesystems")
		return handlePlanningError(err)
	}
	// no progress here since we could run in a live-lock on connectivity issues

	rfss, err := receiver.ListFilesystems(ctx)
	if err != nil {
		log.WithError(err).Error("error listing receiver filesystems")
		return handlePlanningError(err)
	}

	ka.MadeProgress() // for both sender and receiver

	q := make([]*fsrep.Replication, 0, len(sfss))
	mainlog := log
	for _, fs := range sfss {

		log := mainlog.WithField("filesystem", fs.Path)

		log.Debug("assessing filesystem")

		sfsvs, err := sender.ListFilesystemVersions(ctx, fs.Path)
		if err != nil {
			log.WithError(err).Error("cannot get remote filesystem versions")
			return handlePlanningError(err)
		}
		ka.MadeProgress()

		if len(sfsvs) < 1 {
			err := errors.New("sender does not have any versions")
			log.Error(err.Error())
			q = append(q, fsrep.NewReplicationConflictError(fs.Path, err))
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
		ka.MadeProgress()

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
		ka.MadeProgress()
		if path == nil {
			q = append(q, fsrep.NewReplicationConflictError(fs.Path, conflict))
			continue
		}

		var promBytesReplicated *prometheus.CounterVec
		u(func(replication *Replication) { // FIXME args struct like in pruner (also use for sender and receiver)
			promBytesReplicated = replication.promBytesReplicated
		})
		fsrfsm := fsrep.BuildReplication(fs.Path, promBytesReplicated.WithLabelValues(fs.Path))
		if len(path) == 1 {
			fsrfsm.AddStep(nil, path[0])
		} else {
			for i := 0; i < len(path)-1; i++ {
				fsrfsm.AddStep(path[i], path[i+1])
			}
		}
		qitem := fsrfsm.Done()
		ka.MadeProgress()

		log.Debug("compute send size estimate")
		if err = qitem.UpdateSizeEsitmate(ctx, sender); err != nil {
			log.WithError(err).Error("error computing size estimate")
			return handlePlanningError(err)
		}
		ka.MadeProgress()

		q = append(q, qitem)
	}

	ka.MadeProgress()

	return u(func(r *Replication) {
		r.completed = nil
		r.queue = q
		r.err = nil
		r.state = Working
	}).rsf()
}

func statePlanningError(ctx context.Context, ka *watchdog.KeepAlive, sender Sender, receiver Receiver, u updater) state {
	var sleepUntil time.Time
	u(func(r *Replication) {
		sleepUntil = r.sleepUntil
	})
	t := time.NewTimer(sleepUntil.Sub(time.Now()))
	getLogger(ctx).WithField("until", sleepUntil).Info("retry wait after planning error")
	defer t.Stop()
	select {
	case <-ctx.Done():
		return u(func(r *Replication) {
			r.state = PermanentError
			r.err = ctx.Err()
		}).rsf()
	case <-t.C:
	case <-wakeup.Wait(ctx):
	}
	return u(func(r *Replication) {
		r.state = Planning
	}).rsf()
}

type GlobalError struct {
	Err       error
	Temporary bool
}

func (e GlobalError) Error() string {
	errClass := "temporary"
	if !e.Temporary {
		errClass = "permanent"
	}
	return fmt.Sprintf("%s global error: %s", errClass, e.Err)
}

type FilesystemsReplicationFailedError struct {
	FilesystemsWithError []*fsrep.Replication
}

func (e FilesystemsReplicationFailedError) Error() string {
	allSame := true
	lastErr := e.FilesystemsWithError[0].Err().Error()
	for _, fs := range e.FilesystemsWithError {
		fsErr := fs.Err().Error()
		allSame = allSame && lastErr == fsErr
	}

	fsstr := "multiple filesystems"
	if len(e.FilesystemsWithError) == 1 {
		fsstr = fmt.Sprintf("filesystem %s", e.FilesystemsWithError[0].FS())
	}
	errorStr := lastErr
	if !allSame {
		errorStr = "multiple different errors"
	}
	return fmt.Sprintf("%s could not be replicated: %s", fsstr, errorStr)
}

func stateWorking(ctx context.Context, ka *watchdog.KeepAlive, sender Sender, receiver Receiver, u updater) state {

	var active *fsrep.Replication
	rsfNext := u(func(r *Replication) {

		r.err = nil

		newq := make([]*fsrep.Replication, 0, len(r.queue))
		for i := range r.queue {
			if r.queue[i].CanRetry() {
				newq = append(newq, r.queue[i])
			} else {
				r.completed = append(r.completed, r.queue[i])
			}
		}
		sort.SliceStable(newq, func(i, j int) bool {
			return newq[i].NextStepDate().Before(newq[j].NextStepDate())
		})
		r.queue = newq

		if len(r.queue) == 0 {
			r.state = Completed
			fsWithErr := FilesystemsReplicationFailedError{ // prepare it
				FilesystemsWithError: make([]*fsrep.Replication, 0, len(r.completed)),
			}
			for _, fs := range r.completed {
				if fs.CanRetry() {
					panic(fmt.Sprintf("implementation error: completed contains retryable FS %s %#v",
						fs.FS(), fs.Err()))
				}
				if fs.Err() != nil {
					fsWithErr.FilesystemsWithError = append(fsWithErr.FilesystemsWithError, fs)
				}
			}
			if len(fsWithErr.FilesystemsWithError) > 0 {
				r.err = fsWithErr
				r.state = PermanentError
			}
			return
		}

		active =  r.queue[0] // do not dequeue: if it's done, it will be sorted the next time we check for more work
		r.active = active
	}).rsf()

	if active == nil {
		return rsfNext
	}

	activeCtx := fsrep.WithLogger(ctx, getLogger(ctx).WithField("fs", active.FS()))
	err := active.Retry(activeCtx, ka, sender, receiver)
	u(func(r *Replication) {
		r.active = nil
	}).rsf()

	if err != nil {
		if err.LocalToFS() {
			getLogger(ctx).WithError(err).
				Error("filesystem replication encountered a filesystem-specific error")
			// we stay in this state and let the queuing logic above de-prioritize this failing FS
		} else if err.Temporary() {
			getLogger(ctx).WithError(err).
				Error("filesystem encountered a non-filesystem-specific temporary error, enter retry-wait")
			u(func(r *Replication) {
				r.err = GlobalError{Err: err, Temporary: true}
				r.sleepUntil = time.Now().Add(RetryInterval)
				r.state = WorkingWait
			}).rsf()
		} else {
			getLogger(ctx).WithError(err).
				Error("encountered a permanent non-filesystem-specific error")
			u(func(r *Replication) {
				r.err = GlobalError{Err: err, Temporary: false}
				r.state = PermanentError
			}).rsf()
		}
	}

	return u(nil).rsf()
}

func stateWorkingWait(ctx context.Context, ka *watchdog.KeepAlive, sender Sender, receiver Receiver, u updater) state {
	var sleepUntil time.Time
	u(func(r *Replication) {
		sleepUntil = r.sleepUntil
	})
	t := time.NewTimer(RetryInterval)
	getLogger(ctx).WithField("until", sleepUntil).Info("retry wait after error")
	defer t.Stop()
	select {
	case <-ctx.Done():
		return u(func(r *Replication) {
			r.state = PermanentError
			r.err = ctx.Err()
		}).rsf()

	case <-t.C:
	case <-wakeup.Wait(ctx):
	}
	return u(func(r *Replication) {
		r.state = Working
	}).rsf()
}

// Report provides a summary of the progress of the Replication,
// i.e., a condensed dump of the internal state machine.
// Report is safe to be called asynchronously while Drive is running.
func (r *Replication) Report() *Report {
	r.lock.Lock()
	defer r.lock.Unlock()

	rep := Report{
		Status: r.state.String(),
		SleepUntil: r.sleepUntil,
	}

	if r.err != nil {
		rep.Problem = r.err.Error()
	}

	if r.state&(Planning|PlanningError) != 0 {
		return &rep
	}

	rep.Pending = make([]*fsrep.Report, 0, len(r.queue))
	rep.Completed = make([]*fsrep.Report, 0, len(r.completed)) // room for active (potentially)

	// since r.active == r.queue[0], do not contain it in pending output
	pending := r.queue
	if r.active != nil {
		rep.Active = r.active.Report()
		pending = r.queue[1:]
	}
	for _, fsr := range pending {
		rep.Pending= append(rep.Pending, fsr.Report())
	}
	for _, fsr := range r.completed {
		rep.Completed = append(rep.Completed, fsr.Report())
	}

	return &rep
}

func (r *Replication) State() State {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.state
}
