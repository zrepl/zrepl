package driver

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator"
	"github.com/kr/pretty"
	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zrepl/zrepl/daemon/logging/trace"
	"github.com/zrepl/zrepl/zfs"

	"github.com/zrepl/zrepl/replication/report"
	"github.com/zrepl/zrepl/util/chainlock"
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
	config  Config

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

	// ordering relationship that must be maintained for initial replication
	initialRepOrd struct {
		parents, children []*fs
		parentDidUpdate   chan struct{}
	}

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

type Config struct {
	StepQueueConcurrency     int           `validate:"gte=1"`
	MaxAttempts              int           `validate:"eq=-1|gt=0"`
	ReconnectHardFailTimeout time.Duration `validate:"gt=0"`
}

var validate = validator.New()

func (c Config) Validate() error {
	return validate.Struct(c)
}

// caller must ensure config.Validate() == nil
func Do(ctx context.Context, config Config, planner Planner) (ReportFunc, WaitFunc) {

	if err := config.Validate(); err != nil {
		panic(err)
	}

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
		for ano := 0; ano < int(config.MaxAttempts); ano++ {
			log := mainLog.WithField("attempt_number", ano)
			log.Debug("start attempt")

			run.waitReconnect.SetZero()
			run.waitReconnectError = nil

			// do current attempt
			cur := &attempt{
				l:         l,
				startedAt: time.Now(),
				planner:   planner,
				config:    config,
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
				if len(rep.Filesystems) == 0 {
					log.Warn("no filesystems were considered for replication")
				}
				log.Debug("attempt completed")
				break
			}

			mostRecentErr, mostRecentErrClass := errRep.MostRecent()
			log.WithField("most_recent_err", mostRecentErr).WithField("most_recent_err_class", mostRecentErrClass).Debug("most recent error used for re-connect decision")
			if mostRecentErr == nil {
				// inconsistent reporting, let's bail out
				log.WithField("attempt_state", rep.State).Warn("attempt does not report done but error report does not report errors, aborting run")
				break
			}
			log.WithError(mostRecentErr.Err).Error("most recent error in this attempt")
			shouldReconnect := mostRecentErrClass == errorClassTemporaryConnectivityRelated
			log.WithField("reconnect_decision", shouldReconnect).Debug("reconnect decision made")
			if shouldReconnect {
				run.waitReconnect.Set(time.Now(), config.ReconnectHardFailTimeout)
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
	prevs := a.doGlobalPlanning(ctx, prev)
	if prevs == nil {
		return
	}
	a.doFilesystems(ctx, prevs)
}

// if no error occurs, returns a map that maps this attempt's a.fss to `prev`'s a.fss
func (a *attempt) doGlobalPlanning(ctx context.Context, prev *attempt) map[*fs]*fs {
	ctx, endSpan := trace.WithSpan(ctx, "plan")
	defer endSpan()
	pfss, err := a.planner.Plan(ctx)
	errTime := time.Now()
	defer a.l.Lock().Unlock()
	if err != nil {
		a.planErr = newTimedError(err, errTime)
		a.fss = nil
		a.finishedAt = time.Now()
		return nil
	}

	// a.fss != nil indicates that there was no planning error (see doc comment)
	a.fss = make([]*fs, 0)

	for _, pfs := range pfss {
		fs := &fs{
			fs: pfs,
			l:  a.l,
		}
		fs.initialRepOrd.parentDidUpdate = make(chan struct{}, 1)
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
			return nil
		}
		for cur, fss := range prevFSs {
			if len(fss) > 0 {
				prevs[cur] = fss[0]
			}
		}
	}
	// invariant: prevs contains an entry for each unambiguous correspondence

	// build up parent-child relationship (FIXME (O(n^2), but who's going to have that many filesystems...))
	mustDatasetPathOrPlanFail := func(fs string) *zfs.DatasetPath {
		dp, err := zfs.NewDatasetPath(fs)
		if err != nil {
			now := time.Now()
			a.planErr = newTimedError(errors.Wrapf(err, "%q", fs), now)
			a.fss = nil
			a.finishedAt = now
			return nil
		}
		return dp
	}
	for _, f1 := range a.fss {
		fs1 := mustDatasetPathOrPlanFail(f1.fs.ReportInfo().Name)
		if fs1 == nil {
			return nil
		}
		for _, f2 := range a.fss {
			fs2 := mustDatasetPathOrPlanFail(f2.fs.ReportInfo().Name)
			if fs2 == nil {
				return nil
			}
			if fs1.HasPrefix(fs2) && !fs1.Equal(fs2) {
				f1.initialRepOrd.parents = append(f1.initialRepOrd.parents, f2)
				f2.initialRepOrd.children = append(f2.initialRepOrd.children, f1)
			}
		}
	}

	return prevs
}

func (a *attempt) doFilesystems(ctx context.Context, prevs map[*fs]*fs) {
	ctx, endSpan := trace.WithSpan(ctx, "do-repl")
	defer endSpan()

	defer a.l.Lock().Unlock()

	stepQueue := newStepQueue()
	defer stepQueue.Start(a.config.StepQueueConcurrency)()
	var fssesDone sync.WaitGroup
	for _, f := range a.fss {
		fssesDone.Add(1)
		go func(f *fs) {
			defer fssesDone.Done()
			// avoid explosion of tasks with name f.report().Info.Name
			ctx, endTask := trace.WithTaskAndSpan(ctx, "repl-fs", f.report().Info.Name)
			defer endTask()
			f.do(ctx, stepQueue, prevs[f])
		}(f)
	}
	a.l.DropWhile(func() {
		fssesDone.Wait()
	})
	a.finishedAt = time.Now()
}

func (f *fs) debug(format string, args ...interface{}) {
	debugPrefix("fs=%s", f.fs.ReportInfo().Name)(format, args...)
}

// wake up children that watch for f.{planning.{err,done},planned.{step,stepErr}}
func (f *fs) initialRepOrdWakeupChildren() {
	var children []string
	for _, c := range f.initialRepOrd.children {
		// no locking required, c.fs does not change
		children = append(children, c.fs.ReportInfo().Name)
	}
	f.debug("wakeup children %s", children)
	for _, child := range f.initialRepOrd.children {
		select {
		// no locking required, child.initialRepOrd does not change
		case child.initialRepOrd.parentDidUpdate <- struct{}{}:
		default:
		}
	}
}

func (f *fs) do(ctx context.Context, pq *stepQueue, prev *fs) {

	defer f.l.Lock().Unlock()
	defer f.initialRepOrdWakeupChildren()

	// get planned steps from replication logic
	var psteps []Step
	var errTime time.Time
	var err error
	f.l.DropWhile(func() {
		// TODO hacky
		// choose target time that is earlier than any snapshot, so fs planning is always prioritized
		targetDate := time.Unix(0, 0)
		defer pq.WaitReady(ctx, f, targetDate)()
		psteps, err = f.fs.PlanFS(ctx) // no shadow
		errTime = time.Now()           // no shadow
	})
	if err != nil {
		f.planning.err = newTimedError(err, errTime)
		return
	}
	for _, pstep := range psteps {
		step := &step{
			l:    f.l,
			step: pstep,
		}
		f.planned.steps = append(f.planned.steps, step)
	}
	// we're not done planning yet, f.planned.steps might still be changed by next block
	// => don't set f.planning.done just yet
	f.debug("initial len(fs.planned.steps) = %d", len(f.planned.steps))

	// for not-first attempts that succeeded in planning, only allow fs.planned.steps
	// up to and including the originally planned target snapshot
	if prev != nil && prev.planning.done && prev.planning.err == nil {
		f.debug("attempting to correlate plan with previous attempt to find out what is left to do")
		// find the highest of the previously uncompleted steps for which we can also find a step
		// in our current plan
		prevUncompleted := prev.planned.steps[prev.planned.step:]
		var target struct{ prev, cur int }
		target.prev = -1
		target.cur = -1
	out:
		for p := len(prevUncompleted) - 1; p >= 0; p-- {
			for q := len(f.planned.steps) - 1; q >= 0; q-- {
				if prevUncompleted[p].step.TargetEquals(f.planned.steps[q].step) {
					target.prev = p
					target.cur = q
					break out
				}
			}
		}
		if target.prev == -1 || target.cur == -1 {
			f.debug("no correlation possible between previous attempt and this attempt's plan")
			f.planning.err = newTimedError(fmt.Errorf("cannot correlate previously failed attempt to current plan"), time.Now())
			return
		}

		f.planned.steps = f.planned.steps[0:target.cur]
		f.debug("found correlation, new steps are len(fs.planned.steps) = %d", len(f.planned.steps))
	} else {
		f.debug("previous attempt does not exist or did not finish planning, no correlation possible, taking this attempt's plan as is")
	}

	// now we are done planning (f.planned.steps won't change from now on)
	f.planning.done = true

	// wait for parents' initial replication
	var parents []string
	for _, p := range f.initialRepOrd.parents {
		parents = append(parents, p.fs.ReportInfo().Name)
	}
	f.debug("wait for parents %s", parents)
	for {
		var initialReplicatingParentsWithErrors []string
		allParentsPresentOnReceiver := true
		f.l.DropWhile(func() {
			for _, p := range f.initialRepOrd.parents {
				p.l.HoldWhile(func() {
					// (get the preconditions that allow us to inspect p.planned)
					parentHasPlanningDone := p.planning.done && p.planning.err == nil
					if !parentHasPlanningDone {
						// if the parent couldn't be planned, we cannot know whether it needs initial replication
						// or incremental replication => be conservative and assume it was initial replication
						allParentsPresentOnReceiver = false
						if p.planning.err != nil {
							initialReplicatingParentsWithErrors = append(initialReplicatingParentsWithErrors, p.fs.ReportInfo().Name)
						}
						return
					}
					// now allowed to inspect p.planned

					// if there are no steps to be done, the filesystem must exist on the receiving side
					// (otherwise we'd replicate it, and there would be a step for that)
					// (FIXME hardcoded initial replication policy, assuming the policy will always do _some_ initial replication)
					parentHasNoSteps := len(p.planned.steps) == 0

					// OR if it has completed at least one step
					// (remember that .step points to the next step to be done)
					// (TODO technically, we could make this step ready in the moment the recv-side
					//  dataset exists, i.e. after the first few megabytes of transferred data, but we'd have to ask the receiver for that -> poll ListFilesystems RPC)
					parentHasTakenAtLeastOneSuccessfulStep := !parentHasNoSteps && p.planned.step >= 1

					parentFirstStepIsIncremental := // no need to lock for .report() because step.l == it's fs.l
						len(p.planned.steps) > 0 && p.planned.steps[0].report().IsIncremental()

					f.debug("parentHasNoSteps=%v parentFirstStepIsIncremental=%v parentHasTakenAtLeastOneSuccessfulStep=%v",
						parentHasNoSteps, parentFirstStepIsIncremental, parentHasTakenAtLeastOneSuccessfulStep)

					parentPresentOnReceiver := parentHasNoSteps || parentFirstStepIsIncremental || parentHasTakenAtLeastOneSuccessfulStep

					allParentsPresentOnReceiver = allParentsPresentOnReceiver && parentPresentOnReceiver // no shadow

					if !parentPresentOnReceiver && p.planned.stepErr != nil {
						initialReplicatingParentsWithErrors = append(initialReplicatingParentsWithErrors, p.fs.ReportInfo().Name)
					}

				})
			}
		})

		if len(initialReplicatingParentsWithErrors) > 0 {
			f.planned.stepErr = newTimedError(fmt.Errorf("parent(s) failed during initial replication: %s", initialReplicatingParentsWithErrors), time.Now())
			return
		}

		if allParentsPresentOnReceiver {
			break // good to go
		}

		// wait for wakeups from parents, then check again
		// lock must not be held while waiting in order for reporting to work
		f.l.DropWhile(func() {
			select {
			case <-ctx.Done():
				f.planned.stepErr = newTimedError(ctx.Err(), time.Now())
				return
			case <-f.initialRepOrd.parentDidUpdate:
				// loop
			}
		})
		if f.planned.stepErr != nil {
			return
		}
	}

	f.debug("all parents ready, start replication %s", parents)

	// do our steps
	for i, s := range f.planned.steps {
		// lock must not be held while executing step in order for reporting to work
		f.l.DropWhile(func() {
			// wait for parallel replication
			targetDate := s.step.TargetDate()
			defer pq.WaitReady(ctx, f, targetDate)()
			// do the step
			ctx, endSpan := trace.WithSpan(ctx, fmt.Sprintf("%#v", s.step.ReportInfo()))
			defer endSpan()
			err, errTime = s.step.Step(ctx), time.Now() // no shadow
		})

		if err != nil {
			f.planned.stepErr = newTimedError(err, errTime)
			break
		}
		f.planned.step = i + 1 // fs.planned.step must be == len(fs.planned.steps) if all went OK

		f.initialRepOrdWakeupChildren()
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

	var state report.AttemptState
	if a.planErr == nil && a.fss == nil {
		state = report.AttemptPlanning
	} else if a.planErr != nil && a.fss == nil {
		state = report.AttemptPlanningError
	} else if a.planErr == nil && a.fss != nil {
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
	} else {
		panic(fmt.Sprintf("attempt.planErr and attempt.fss must not both be != nil:\n%s\n%s", pretty.Sprint(a.planErr), pretty.Sprint(a.fss)))
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
