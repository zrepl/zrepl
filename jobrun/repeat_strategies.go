package jobrun

import (
	"time"
)

type NoRepeatStrategy struct{}

func (s NoRepeatStrategy) ShouldReschedule(lastResult JobRunResult) (time.Time, bool) {
	return time.Time{}, false
}

type PeriodicRepeatStrategy struct {
	Interval time.Duration
}

func (s *PeriodicRepeatStrategy) ShouldReschedule(lastResult JobRunResult) (next time.Time, shouldRun bool) {
	// Don't care about the result
	shouldRun = true
	next = lastResult.Start.Add(s.Interval)
	if next.Before(time.Now()) {
		next = time.Now()
	}
	return
}
