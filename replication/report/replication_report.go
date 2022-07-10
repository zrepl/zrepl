package report

import (
	"encoding/json"
	"time"
)

type Report struct {
	StartAt, FinishAt                      time.Time
	WaitReconnectSince, WaitReconnectUntil time.Time
	WaitReconnectError                     *TimedError
	Attempts                               []*AttemptReport
}

var _, _ = json.Marshal(&Report{})

type TimedError struct {
	Err  string
	Time time.Time
}

func NewTimedError(err string, t time.Time) *TimedError {
	if err == "" {
		panic("error must be empty")
	}
	if t.IsZero() {
		panic("t must be non-zero")
	}
	return &TimedError{err, t}
}

func (s *TimedError) Error() string {
	return s.Err
}

var _, _ = json.Marshal(&TimedError{})

type AttemptReport struct {
	State             AttemptState
	StartAt, FinishAt time.Time
	PlanError         *TimedError
	Filesystems       []*FilesystemReport
}

type AttemptState string

const (
	AttemptPlanning      AttemptState = "planning"
	AttemptPlanningError AttemptState = "planning-error"
	AttemptFanOutFSs     AttemptState = "fan-out-filesystems"
	AttemptFanOutError   AttemptState = "filesystem-error"
	AttemptDone          AttemptState = "done"
)

type FilesystemState string

const (
	FilesystemPlanning        FilesystemState = "planning"
	FilesystemPlanningErrored FilesystemState = "planning-error"
	FilesystemStepping        FilesystemState = "stepping"
	FilesystemSteppingErrored FilesystemState = "step-error"
	FilesystemDone            FilesystemState = "done"
)

type FsBlockedOn string

const (
	FsBlockedOnNothing           FsBlockedOn = "nothing"
	FsBlockedOnPlanningStepQueue FsBlockedOn = "plan-queue"
	FsBlockedOnParentInitialRepl FsBlockedOn = "parent-initial-repl"
	FsBlockedOnReplStepQueue     FsBlockedOn = "repl-queue"
)

type FilesystemReport struct {
	Info *FilesystemInfo

	State FilesystemState

	// Always valid.
	BlockedOn FsBlockedOn

	// Valid in State = FilesystemPlanningErrored
	PlanError *TimedError
	// Valid in State = FilesystemSteppingErrored
	StepError *TimedError

	// Valid in State = FilesystemStepping
	CurrentStep int
	Steps       []*StepReport
}

type FilesystemInfo struct {
	Name string
}

type StepReport struct {
	Info *StepInfo
}

type StepInfo struct {
	From, To        string
	Resumed         bool
	BytesExpected   uint64
	BytesReplicated uint64
}

func (a *AttemptReport) BytesSum() (expected, replicated uint64, containsInvalidSizeEstimates bool) {
	for _, fs := range a.Filesystems {
		e, r, fsContainsInvalidEstimate := fs.BytesSum()
		containsInvalidSizeEstimates = containsInvalidSizeEstimates || fsContainsInvalidEstimate
		expected += e
		replicated += r
	}
	return expected, replicated, containsInvalidSizeEstimates
}

func (f *FilesystemReport) BytesSum() (expected, replicated uint64, containsInvalidSizeEstimates bool) {
	for _, step := range f.Steps {
		expected += step.Info.BytesExpected
		replicated += step.Info.BytesReplicated
		containsInvalidSizeEstimates = containsInvalidSizeEstimates || step.Info.BytesExpected == 0
	}
	return
}

func (f *AttemptReport) FilesystemsByState() map[FilesystemState][]*FilesystemReport {
	r := make(map[FilesystemState][]*FilesystemReport, 4)
	for _, fs := range f.Filesystems {
		l := r[fs.State]
		l = append(l, fs)
		r[fs.State] = l
	}
	return r
}

func (f *FilesystemReport) Error() *TimedError {
	switch f.State {
	case FilesystemPlanningErrored:
		return f.PlanError
	case FilesystemSteppingErrored:
		return f.StepError
	}
	return nil
}

// may return nil
func (f *FilesystemReport) NextStep() *StepReport {
	switch f.State {
	case FilesystemDone:
		return nil
	case FilesystemPlanningErrored:
		return nil
	case FilesystemSteppingErrored:
		return nil
	case FilesystemPlanning:
		return nil
	case FilesystemStepping:
		// invariant is that this is always correct
		// TODO what about 0-length Steps but short intermediary state?
		return f.Steps[f.CurrentStep]
	}
	panic("unreachable")
}

func (f *StepReport) IsIncremental() bool {
	return f.Info.From != ""
}

// Returns, for the latest replication attempt,
// 0  if there have not been any replication attempts,
// -1 if the replication failed while enumerating file systems
// N  if N filesystems could not not be replicated successfully
func (r *Report) GetFailedFilesystemsCountInLatestAttempt() int {

	if len(r.Attempts) == 0 {
		return 0
	}

	a := r.Attempts[len(r.Attempts)-1]
	switch a.State {
	case AttemptPlanningError:
		return -1
	case AttemptFanOutError:
		var count int
		for _, f := range a.Filesystems {
			if f.Error() != nil {
				count++
			}
		}
		return count
	default:
		return 0
	}
}
