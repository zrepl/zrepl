package driver

import (
	"context"
	"sync"
	"time"

	"github.com/zrepl/zrepl/replication/report"
	"github.com/zrepl/zrepl/util/chainlock"
)

type run struct {
	l *chainlock.L

	startedAt, finishedAt time.Time

	// the attempts attempted so far:
	// All but the last in this slice must have finished with some errors.
	// The last attempt may not be finished and may not have errors.
	attempts []*attempt
}

type Planner interface {
	Plan(context.Context) ([]FS, error)
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
	PlanFS(context.Context) ([]Step, error)
	ReportInfo() *report.FilesystemInfo
}

type Step interface {
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

func Do(ctx context.Context, planner Planner) (ReportFunc, WaitFunc) {
	l := chainlock.New()
	run := &run{
		l:         l,
		startedAt: time.Now(),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)

		run.l.Lock()
		a1 := &attempt{
			l:         l,
			startedAt: time.Now(),
			planner:   planner,
		}
		run.attempts = append(run.attempts, a1)
		run.l.Unlock()
		a1.do(ctx)

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

func (a *attempt) do(ctx context.Context) {
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

	stepQueue := newStepQueue()
	defer stepQueue.Start(1)()
	var fssesDone sync.WaitGroup
	for _, f := range a.fss {
		fssesDone.Add(1)
		go func(f *fs) {
			defer fssesDone.Done()
			f.do(ctx, stepQueue)
		}(f)
	}
	a.l.DropWhile(func() {
		fssesDone.Wait()
	})
	a.finishedAt = time.Now()
}

func (fs *fs) do(ctx context.Context, pq *stepQueue) {
	psteps, err := fs.fs.PlanFS(ctx)
	errTime := time.Now()
	defer fs.l.Lock().Unlock()
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
		Attempts: make([]*report.AttemptReport, len(r.attempts)),
		StartAt:  r.startedAt,
		FinishAt: r.finishedAt,
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
