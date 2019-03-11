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

type FilesystemReport struct {
	Info *FilesystemInfo

	State FilesystemState

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
	BytesExpected   int64
	BytesReplicated int64
}

func (a *AttemptReport) BytesSum() (expected, replicated int64) {
	for _, fs := range a.Filesystems {
		e, r := fs.BytesSum()
		expected += e
		replicated += r
	}
	return expected, replicated
}

func (f *FilesystemReport) BytesSum() (expected, replicated int64) {
	for _, step := range f.Steps {
		expected += step.Info.BytesExpected
		replicated += step.Info.BytesReplicated
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
	return f.Info.From != "" // FIXME change to ZFS semantics (To != "")
}
