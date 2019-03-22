package driver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zrepl/zrepl/replication/report"
	"github.com/zrepl/zrepl/util/chainlock"
	"github.com/zrepl/zrepl/util/envconst"
)

type interval struct {
	begin time.Time
	end   time.Time
}

func (w *interval) SetZero() {
	w.begin = time.Time{}
	w.end = time.Time{}
}

// Duration of 0 means indefinite length
func (w *interval) Set(begin time.Time, duration time.Duration) {
	if begin.IsZero() {
		panic("zero begin time now allowed")
	}
	w.begin = begin
	w.end = begin.Add(duration)
}

// Returns the End of the interval if it has a defined length.
// For indefinite lengths, returns the zero value.
func (w *interval) End() time.Time {
	return w.end
}

// Return a context with a deadline at the interval's end.
// If the interval has indefinite length (duration 0 on Set), return ctx as is.
// The returned context.CancelFunc can be called either way.
func (w *interval) ContextWithDeadlineAtEnd(ctx context.Context) (context.Context, context.CancelFunc) {
	if w.begin.IsZero() {
		panic("must call Set before ContextWIthDeadlineAtEnd")
	}
	if w.end.IsZero() {
		// indefinite length, just return context as is
		return ctx, func() {}
	} else {
		return context.WithDeadline(ctx, w.end)
	}
}

type run struct {
	l *chainlock.L

	startedAt, finishedAt time.Time

	waitReconnect      interval
	waitReconnectError *timedError

	// the attempts attempted so far:
	// All but the last in this slice must have finished with some errors.
	// The last attempt may not be finished and may not have errors.
	attempts []*attempt
}

type Planner interface {
	Plan(context.Context) ([]FS, error)
	WaitForConnectivity(context.Context) error
}

// an attempt represents a single planning & execution of fs replications
type attempt struct {
	planner Planner

	l *chainlock.L

	startedAt, finishedAt time.Time

	// after Planner.Plan was called, planErr and fss are mutually exclusive with regards to nil-ness
	// if both are nil, it must be assumed that Planner.Plan is active
	planErr *timedError
	fss     []*fs
}

type timedError struct {
	Err  error
	Time time.Time
}

func newTimedError(err error, t time.Time) *timedError {
	if err == nil {
		panic("error must be non-nil")
	}
	if t.IsZero() {
		panic("t must be non-zero")
	}
	return &timedError{err, t}
}

func (e *timedError) IntoReportError() *report.TimedError {
	if e == nil {
		return nil
	}
	return report.NewTimedError(e.Err.Error(), e.Time)
}

type FS interface {
	// Returns true if this FS and fs refer to the same filesystem returned
	// by Planner.Plan in a previous attempt.
	EqualToPreviousAttempt(fs FS) bool
	// The returned steps are assumed to be dependent on exactly
	// their direct predecessors in the returned list.
	PlanFS(context.Context) ([]Step, error)
	ReportInfo() *report.FilesystemInfo
}

type Step interface {
	// Returns true iff the target snapshot is the same for this Step and other.
	// We do not use TargetDate to avoid problems with wrong system time on
	// snapshot creation.
	//
	// Implementations can assume that `other` is a step of the same filesystem,
	// although maybe from a previous attempt.
	// (`same` as defined by FS.EqualToPreviousAttempt)
	//
	// Note that TargetEquals should return true in a situation with one
	// originally sent snapshot and a subsequent attempt's step that uses
	// resumable send & recv.
	TargetEquals(other Step) bool
	TargetDate() time.Time
	Step(context.Context) error
	ReportInfo() *report.StepInfo
}

type fs struct {
	fs FS

	l *chainlock.L

	planning struct {
		done bool
		err  *timedError
	}

	// valid iff planning.done && planning.err == nil
	planned struct {
		// valid iff planning.done && planning.err == nil
		stepErr *timedError
		// all steps, in the order in which they must be completed
		steps []*step
		// index into steps, pointing at the step that is currently executing
		// if step >= len(steps), no more work needs to be done
		step int
	}
}

type step struct {
	l    *chainlock.L
	step Step
}

type ReportFunc func() *report.Report
type WaitFunc func(block bool) (done bool)

var maxAttempts = envconst.Int64("ZREPL_REPLICATION_MAX_ATTEMPTS", 3)
var reconnectHardFailTimeout = envconst.Duration("ZREPL_REPLICATION_RECONNECT_HARD_FAIL_TIMEOUT", 10*time.Minute)

func Do(ctx context.Context, planner Planner) (ReportFunc, WaitFunc) {
	log := getLog(ctx)
	l := chainlock.New()
	run := &run{
		l:         l,
		startedAt: time.Now(),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)

		defer run.l.Lock().Unlock()
		log.Debug("begin run")
		defer log.Debug("run ended")
		var prev *attempt
		mainLog := log
		for ano := 0; ano < int(maxAttempts) || maxAttempts == 0; ano++ {
			log := mainLog.WithField("attempt_number", ano)
			log.Debug("start attempt")

			run.waitReconnect.SetZero()
			run.waitReconnectError = nil

			// do current attempt
			cur := &attempt{
				l:         l,
				startedAt: time.Now(),
				planner:   planner,
			}
			run.attempts = append(run.attempts, cur)
			run.l.DropWhile(func() {
				cur.do(ctx, prev)
			})
			prev = cur
			if ctx.Err() != nil {
				log.WithError(ctx.Err()).Info("context error")
				return
			}

			// error classification, bail out if done / permanent error
			rep := cur.report()
			log.WithField("attempt_state", rep.State).Debug("attempt state")
			errRep := cur.errorReport()

			if rep.State == report.AttemptDone {
				log.Debug("attempt completed successfully")
				break
			}

			mostRecentErr, mostRecentErrClass := errRep.MostRecent()
			log.WithField("most_recent_err", mostRecentErr).WithField("most_recent_err_class", mostRecentErrClass).Debug("most recent error used for re-connect decision")
			if mostRecentErr == nil {
				// inconsistent reporting, let's bail out
				log.Warn("attempt does not report done but error report does not report errors, aborting run")
				break
			}
			log.WithError(mostRecentErr.Err).Error("most recent error in this attempt")
			shouldReconnect := mostRecentErrClass == errorClassTemporaryConnectivityRelated
			log.WithField("reconnect_decision", shouldReconnect).Debug("reconnect decision made")
			if shouldReconnect {
				run.waitReconnect.Set(time.Now(), reconnectHardFailTimeout)
				log.WithField("deadline", run.waitReconnect.End()).Error("temporary connectivity-related error identified, start waiting for reconnect")
				var connectErr error
				var connectErrTime time.Time
				run.l.DropWhile(func() {
					ctx, cancel := run.waitReconnect.ContextWithDeadlineAtEnd(ctx)
					defer cancel()
					connectErr = planner.WaitForConnectivity(ctx)
					connectErrTime = time.Now()
				})
				if connectErr == nil {
					log.Error("reconnect successful") // same level as 'begin with reconnect' message above
					continue
				} else {
					run.waitReconnectError = newTimedError(connectErr, connectErrTime)
					log.WithError(connectErr).Error("reconnecting failed, aborting run")
					break
				}
			} else {
				log.Error("most recent error cannot be solved by reconnecting, aborting run")
				return
			}

		}

	}()

	wait := func(block bool) bool {
		if block {
			<-done
		}
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	report := func() *report.Report {
		defer run.l.Lock().Unlock()
		return run.report()
	}
	return report, wait
}

func (a *attempt) do(ctx context.Context, prev *attempt) {
	pfss, err := a.planner.Plan(ctx)
	errTime := time.Now()
	defer a.l.Lock().Unlock()
	if err != nil {
		a.planErr = newTimedError(err, errTime)
		a.fss = nil
		a.finishedAt = time.Now()
		return
	}

	for _, pfs := range pfss {
		fs := &fs{
			fs: pfs,
			l:  a.l,
		}
		a.fss = append(a.fss, fs)
	}

	prevs := make(map[*fs]*fs)
	{
		prevFSs := make(map[*fs][]*fs, len(pfss))
		if prev != nil {
			debug("previous attempt has %d fss", len(a.fss))
			for _, fs := range a.fss {
				for _, prevFS := range prev.fss {
					if fs.fs.EqualToPreviousAttempt(prevFS.fs) {
						l := prevFSs[fs]
						l = append(l, prevFS)
						prevFSs[fs] = l
					}
				}
			}
		}
		type inconsistency struct {
			cur   *fs
			prevs []*fs
		}
		var inconsistencies []inconsistency
		for cur, fss := range prevFSs {
			if len(fss) > 1 {
				inconsistencies = append(inconsistencies, inconsistency{cur, fss})
			}
		}
		sort.SliceStable(inconsistencies, func(i, j int) bool {
			return inconsistencies[i].cur.fs.ReportInfo().Name < inconsistencies[j].cur.fs.ReportInfo().Name
		})
		if len(inconsistencies) > 0 {
			var msg strings.Builder
			msg.WriteString("cannot determine filesystem correspondences between different attempts:\n")
			var inconsistencyLines []string
			for _, i := range inconsistencies {
				var prevNames []string
				for _, prev := range i.prevs {
					prevNames = append(prevNames, prev.fs.ReportInfo().Name)
				}
				l := fmt.Sprintf("  %s => %v", i.cur.fs.ReportInfo().Name, prevNames)
				inconsistencyLines = append(inconsistencyLines, l)
			}
			fmt.Fprint(&msg, strings.Join(inconsistencyLines, "\n"))
			now := time.Now()
			a.planErr = newTimedError(errors.New(msg.String()), now)
			a.fss = nil
			a.finishedAt = now
			return
		}
		for cur, fss := range prevFSs {
			if len(fss) > 0 {
				prevs[cur] = fss[0]
			}
		}
	}
	// invariant: prevs contains an entry for each unambigious correspondence

	stepQueue := newStepQueue()
	defer stepQueue.Start(1)() // TODO parallel replication
	var fssesDone sync.WaitGroup
	for _, f := range a.fss {
		fssesDone.Add(1)
		go func(f *fs) {
			defer fssesDone.Done()
			f.do(ctx, stepQueue, prevs[f])
		}(f)
	}
	a.l.DropWhile(func() {
		fssesDone.Wait()
	})
	a.finishedAt = time.Now()
}

func (fs *fs) do(ctx context.Context, pq *stepQueue, prev *fs) {
	psteps, err := fs.fs.PlanFS(ctx)
	errTime := time.Now()
	defer fs.l.Lock().Unlock()
	debug := debugPrefix("fs=%s", fs.fs.ReportInfo().Name)
	fs.planning.done = true
	if err != nil {
		fs.planning.err = newTimedError(err, errTime)
		return
	}
	for _, pstep := range psteps {
		step := &step{
			l:    fs.l,
			step: pstep,
		}
		fs.planned.steps = append(fs.planned.steps, step)
	}
	debug("iniital len(fs.planned.steps) = %d", len(fs.planned.steps))

	// for not-first attempts, only allow fs.planned.steps
	// up to including the originally planned target snapshot
	if prev != nil && prev.planning.done && prev.planning.err == nil {
		prevUncompleted := prev.planned.steps[prev.planned.step:]
		if len(prevUncompleted) == 0 {
			debug("prevUncompleted is empty")
			return
		}
		if len(fs.planned.steps) == 0 {
			debug("fs.planned.steps is empty")
			return
		}
		prevFailed := prevUncompleted[0]
		curFirst := fs.planned.steps[0]
		// we assume that PlanFS retries prevFailed (using curFirst)
		if !prevFailed.step.TargetEquals(curFirst.step) {
			debug("Targets don't match")
			// Two options:
			// A: planning algorithm is broken
			// B: manual user intervention inbetween
			// Neither way will we make progress, so let's error out
			stepFmt := func(step *step) string {
				r := step.report()
				s := r.Info
				if r.IsIncremental() {
					return fmt.Sprintf("%s=>%s", s.From, s.To)
				} else {
					return fmt.Sprintf("full=>%s", s.To)
				}
			}
			msg := fmt.Sprintf("last attempt's uncompleted step %s does not correspond to this attempt's first planned step %s",
				stepFmt(prevFailed), stepFmt(curFirst))
			fs.planned.stepErr = newTimedError(errors.New(msg), time.Now())
			return
		}
		// only allow until step targets diverge
		min := len(prevUncompleted)
		if min > len(fs.planned.steps) {
			min = len(fs.planned.steps)
		}
		diverge := 0
		for ; diverge < min; diverge++ {
			debug("diverge compare iteration %d", diverge)
			if !fs.planned.steps[diverge].step.TargetEquals(prevUncompleted[diverge].step) {
				break
			}
		}
		debug("diverge is %d", diverge)
		fs.planned.steps = fs.planned.steps[0:diverge]
	}
	debug("post-prev-merge len(fs.planned.steps) = %d", len(fs.planned.steps))

	for i, s := range fs.planned.steps {
		var (
			err     error
			errTime time.Time
		)
		// lock must not be held while executing step in order for reporting to work
		fs.l.DropWhile(func() {
			targetDate := s.step.TargetDate()
			defer pq.WaitReady(fs, targetDate)()
			err = s.step.Step(ctx) // no shadow
			errTime = time.Now()   // no shadow
		})
		if err != nil {
			fs.planned.stepErr = newTimedError(err, errTime)
			break
		}
		fs.planned.step = i + 1 // fs.planned.step must be == len(fs.planned.steps) if all went OK
	}
}

// caller must hold lock l
func (r *run) report() *report.Report {
	report := &report.Report{
		Attempts:           make([]*report.AttemptReport, len(r.attempts)),
		StartAt:            r.startedAt,
		FinishAt:           r.finishedAt,
		WaitReconnectSince: r.waitReconnect.begin,
		WaitReconnectUntil: r.waitReconnect.end,
		WaitReconnectError: r.waitReconnectError.IntoReportError(),
	}
	for i := range report.Attempts {
		report.Attempts[i] = r.attempts[i].report()
	}
	return report
}

// caller must hold lock l
func (a *attempt) report() *report.AttemptReport {

	r := &report.AttemptReport{
		// State is set below
		Filesystems: make([]*report.FilesystemReport, len(a.fss)),
		StartAt:     a.startedAt,
		FinishAt:    a.finishedAt,
		PlanError:   a.planErr.IntoReportError(),
	}

	for i := range r.Filesystems {
		r.Filesystems[i] = a.fss[i].report()
	}

	state := report.AttemptPlanning
	if a.planErr != nil {
		state = report.AttemptPlanningError
	} else if a.fss != nil {
		if a.finishedAt.IsZero() {
			state = report.AttemptFanOutFSs
		} else {
			fsWithError := false
			for _, s := range r.Filesystems {
				fsWithError = fsWithError || s.Error() != nil
			}
			state = report.AttemptDone
			if fsWithError {
				state = report.AttemptFanOutError
			}
		}
	}
	r.State = state

	return r
}

// caller must hold lock l
func (f *fs) report() *report.FilesystemReport {
	state := report.FilesystemPlanningErrored
	if f.planning.err == nil {
		if f.planning.done {
			if f.planned.stepErr != nil {
				state = report.FilesystemSteppingErrored
			} else if f.planned.step < len(f.planned.steps) {
				state = report.FilesystemStepping
			} else {
				state = report.FilesystemDone
			}
		} else {
			state = report.FilesystemPlanning
		}
	}
	r := &report.FilesystemReport{
		Info:        f.fs.ReportInfo(),
		State:       state,
		PlanError:   f.planning.err.IntoReportError(),
		StepError:   f.planned.stepErr.IntoReportError(),
		Steps:       make([]*report.StepReport, len(f.planned.steps)),
		CurrentStep: f.planned.step,
	}
	for i := range r.Steps {
		r.Steps[i] = f.planned.steps[i].report()
	}
	return r
}

// caller must hold lock l
func (s *step) report() *report.StepReport {
	r := &report.StepReport{
		Info: s.step.ReportInfo(),
	}
	return r
}

//go:generate enumer -type=errorClass
type errorClass int

const (
	errorClassPermanent errorClass = iota
	errorClassTemporaryConnectivityRelated
)

type errorReport struct {
	flattened []*timedError
	// sorted DESCending by err time
	byClass map[errorClass][]*timedError
}

// caller must hold lock l
func (a *attempt) errorReport() *errorReport {
	r := &errorReport{}
	if a.planErr != nil {
		r.flattened = append(r.flattened, a.planErr)
	}
	for _, fs := range a.fss {
		if fs.planning.done && fs.planning.err != nil {
			r.flattened = append(r.flattened, fs.planning.err)
		} else if fs.planning.done && fs.planned.stepErr != nil {
			r.flattened = append(r.flattened, fs.planned.stepErr)
		}
	}

	// build byClass
	{
		r.byClass = make(map[errorClass][]*timedError)
		putClass := func(err *timedError, class errorClass) {
			errs := r.byClass[class]
			errs = append(errs, err)
			r.byClass[class] = errs
		}
		for _, err := range r.flattened {
			if neterr, ok := err.Err.(net.Error); ok && neterr.Temporary() {
				putClass(err, errorClassTemporaryConnectivityRelated)
				continue
			}
			if st, ok := status.FromError(err.Err); ok && st.Code() == codes.Unavailable {
				// technically, codes.Unavailable could be returned by the gRPC endpoint, indicating overload, etc.
				// for now, let's assume it only happens for connectivity issues, as specified in
				// https://grpc.io/grpc/core/md_doc_statuscodes.html
				putClass(err, errorClassTemporaryConnectivityRelated)
				continue
			}
			putClass(err, errorClassPermanent)
		}
		for _, errs := range r.byClass {
			sort.Slice(errs, func(i, j int) bool {
				return errs[i].Time.After(errs[j].Time) // sort descendingly
			})
		}
	}

	return r
}

func (r *errorReport) AnyError() *timedError {
	for _, err := range r.flattened {
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *errorReport) MostRecent() (err *timedError, errClass errorClass) {
	for class, errs := range r.byClass {
		// errs are sorted descendingly during construction
		if len(errs) > 0 && (err == nil || errs[0].Time.After(err.Time)) {
			err = errs[0]
			errClass = class
		}
	}
	return
}
