package util

import (
	"context"
	"sync"
	"time"
)

type contextWithOptionalDeadline struct {
	context.Context

	m        sync.Mutex
	deadline time.Time

	done chan struct{}
	err  error
}

func (c *contextWithOptionalDeadline) Deadline() (deadline time.Time, ok bool) {
	c.m.Lock()
	defer c.m.Unlock()
	return c.deadline, !c.deadline.IsZero()
}

func (c *contextWithOptionalDeadline) Err() error {
	c.m.Lock()
	defer c.m.Unlock()
	return c.err
}

func (c *contextWithOptionalDeadline) Done() <-chan struct{} {
	return c.done
}

func ContextWithOptionalDeadline(pctx context.Context) (ctx context.Context, enforceDeadline func(deadline time.Time)) {

	// mctx can only be cancelled by cancelMctx, not by a potential cancel of pctx
	rctx := &contextWithOptionalDeadline{
		Context: pctx,
		done:    make(chan struct{}),
		err:     nil,
	}
	enforceDeadline = func(deadline time.Time) {

		// Set deadline and prohibit multiple calls
		rctx.m.Lock()
		alreadyCalled := !rctx.deadline.IsZero()
		if !alreadyCalled {
			rctx.deadline = deadline
		}
		rctx.m.Unlock()
		if alreadyCalled {
			return
		}

		// Deadline in past?
		sleepTime := deadline.Sub(time.Now())
		if sleepTime <= 0 {
			rctx.m.Lock()
			rctx.err = context.DeadlineExceeded
			rctx.m.Unlock()
			close(rctx.done)
			return
		}
		go func() {
			// Set a timer and wait for timer or parent context to be cancelled
			timer := time.NewTimer(sleepTime)
			var setErr error
			select {
			case <-pctx.Done():
				timer.Stop()
				setErr = pctx.Err()
			case <-timer.C:
				setErr = context.DeadlineExceeded
			}
			rctx.m.Lock()
			rctx.err = setErr
			rctx.m.Unlock()
			close(rctx.done)
		}()
	}
	return rctx, enforceDeadline
}
