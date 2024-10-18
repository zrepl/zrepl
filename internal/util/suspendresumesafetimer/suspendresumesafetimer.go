package suspendresumesafetimer

import (
	"context"
	"time"

	"github.com/zrepl/zrepl/internal/util/envconst"
)

// The returned error is guaranteed to be the ctx.Err()
func SleepUntil(ctx context.Context, sleepUntil time.Time) error {

	// We use .Round(0) to strip the monotonic clock reading from the time.Time
	// returned by time.Now(). That will make the before/after check in the ticker
	// for-loop compare wall-clock times instead of monotonic time.
	// Comparing wall clock time is necessary because monotonic time does not progress
	// while the system is suspended.
	//
	// Background
	//
	// A time.Time carries a wallclock timestamp and optionally a monotonic clock timestamp.
	// time.Now() returns a time.Time that carries both.
	// time.Time.Add() applies the same delta to both timestamps in the time.Time.
	// x.Sub(y) will return the *monotonic* delta if both x and y carry a monotonic timestamp.
	// time.Until(x) == x.Sub(now) where `now` will have a monotonic timestamp.
	// So, time.Until(x) with an `x` that has monotonic timestamp will return monotonic delta.
	//
	// Why Do We Care?
	//
	// On systems that suspend/resume, wall clock time progresses during suspend but
	// monotonic time does not.
	//
	// So, suppose the following sequence of events:
	//   x <== time.Now()
	//   System suspends for 1 hour
	//   delta <== time.Now().Sub(x)
	// `delta` will be near 0 because time.Until() subtracts the monotonic
	// timestamps, and monotonic time didn't progress during suspend.
	//
	// Now strip the timestamp using .Round(0)
	//   x <== time.Now().Round(0)
	//   System suspends for 1 hour
	//   delta <== time.Now().Sub(x)
	// `delta` will be 1 hour because time.Sub() subtracted wallclock timestamps
	// because x didn't have a monotonic timestamp because we stripped it using .Round(0).
	//
	//
	sleepUntil = sleepUntil.Round(0)

	// Set up a timer so that, if the system doesn't suspend/resume,
	// we get a precise wake-up time from the native Go timer.
	monotonicClockTimer := time.NewTimer(time.Until(sleepUntil))
	defer func() {
		if !monotonicClockTimer.Stop() {
			// non-blocking read since we can come here when
			// we've already drained the channel through
			//		case <-monotonicClockTimer.C
			// in the `for` loop below.
			select {
			case <-monotonicClockTimer.C:
			default:
			}
		}
	}()

	// Set up a ticker so that we're guaranteed to wake up periodically.
	// We'll then get the current wall-clock time and check ourselves
	// whether we're past the requested expiration time.
	// Pick a 10 second check interval by default since it's rare that
	// suspend/resume is done more frequently.
	ticker := time.NewTicker(envconst.Duration("ZREPL_WALLCLOCKTIMER_MAX_DELAY", 10*time.Second))
	defer ticker.Stop()

	for {
		select {
		case <-monotonicClockTimer.C:
			return nil
		case <-ticker.C:
			now := time.Now()
			if now.Before(sleepUntil) {
				// Continue waiting.

				// Reset the monotonic timer to reset drift.
				if !monotonicClockTimer.Stop() {
					<-monotonicClockTimer.C
				}
				monotonicClockTimer.Reset(time.Until(sleepUntil))

				continue
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
