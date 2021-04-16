//
//
// Alternative Design (in "RustGo")
//
//	enum InternalMsg {
//	    Trigger((), chan (TriggerResponse, error)),
//	    Poll(PollRequest, chan PollResponse),
//	    Reset(ResetRequest, chan (ResetResponse, error)),
//	}
//
//	enum State {
//	    Running{
//	        invocationId: u32,
//	        cancelCurrentInvocation: context.CancelFunc
//	    }
//	    Waiting{
//	        nextInvocationId: u32,
//	    }
//	}
//
//	for msg := <- t.internalMsgs {
//	    match (msg, state) {
//	        ...
//	    }
//	}
package trigger

import (
	"context"
	"fmt"
	"math"
	"sync"
)

type T struct {
	mtx sync.Mutex
	cv  sync.Cond

	nextInvocationId        uint64
	activeInvocationId      uint64 // 0 <=> inactive
	triggerPending          bool
	contextDone             bool
	reset                   chan uint64
	stopWaitForReset        chan struct{}
	cancelCurrentInvocation context.CancelFunc
}

func New() *T {
	t := &T{
		activeInvocationId: math.MaxUint64,
		nextInvocationId:   1,
	}
	t.cv.L = &t.mtx
	return t
}

func (t *T) WaitForTrigger(ctx context.Context) (rctx context.Context, err error) {
	t.mtx.Lock()
	defer t.mtx.Unlock()

	if t.activeInvocationId == 0 {
		return nil, fmt.Errorf("must be running when calling this function")
	}
	t.activeInvocationId = 0
	t.cancelCurrentInvocation = nil

	if t.contextDone == true {
		panic("implementation error: this variable is only true while in WaitForTrigger, and that's a mutually exclusive function")
	}
	stopWaitingForDone := make(chan struct{})
	go func() {
		select {
		case <-stopWaitingForDone:
		case <-ctx.Done():
			t.mtx.Lock()
			t.contextDone = true
			t.cv.Broadcast()
			t.mtx.Unlock()
		}
	}()

	defer func() {
		t.triggerPending = false
		t.contextDone = false
	}()
	for !t.triggerPending && !t.contextDone {
		t.cv.Wait()
	}
	close(stopWaitingForDone)
	if t.contextDone {
		if ctx.Err() == nil {
			panic("implementation error: contextDone <=> ctx.Err() != nil")
		}
		return nil, ctx.Err()
	}

	t.activeInvocationId = t.nextInvocationId
	t.nextInvocationId++
	rctx, t.cancelCurrentInvocation = context.WithCancel(ctx)

	return rctx, nil
}

type TriggerResponse struct {
	InvocationId uint64
}

func (t *T) Trigger() (TriggerResponse, error) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	var invocationId uint64
	if t.activeInvocationId != 0 {
		invocationId = t.activeInvocationId
	} else {
		invocationId = t.nextInvocationId
	}
	// non-blocking send (.Run() must not hold mutex while waiting for signals)
	t.triggerPending = true
	t.cv.Broadcast()
	return TriggerResponse{InvocationId: invocationId}, nil
}

type PollRequest struct {
	InvocationId uint64
}

type PollResponse struct {
	Done         bool
	InvocationId uint64
}

func (t *T) Poll(req PollRequest) (res PollResponse) {
	t.mtx.Lock()
	defer t.mtx.Unlock()

	waitForId := req.InvocationId
	if req.InvocationId == 0 {
		// handle the case where the client doesn't know what the current invocation id is
		if t.activeInvocationId != 0 {
			waitForId = t.activeInvocationId
		} else {
			waitForId = t.nextInvocationId
		}
	}

	var done bool
	if t.activeInvocationId == 0 {
		done = waitForId < t.nextInvocationId
	} else {
		done = waitForId < t.activeInvocationId
	}
	return PollResponse{Done: done, InvocationId: waitForId}
}

type ResetRequest struct {
	InvocationId uint64
}

type ResetResponse struct {
	InvocationId uint64
}

func (t *T) Reset(req ResetRequest) (*ResetResponse, error) {
	t.mtx.Lock()
	defer t.mtx.Unlock()

	resetId := req.InvocationId
	if req.InvocationId == 0 {
		// handle the case where the client doesn't know what the current invocation id is
		resetId = t.activeInvocationId
	}

	if resetId == 0 {
		return nil, fmt.Errorf("no active invocation")
	}

	if resetId != t.activeInvocationId {
		return nil, fmt.Errorf("active invocation (%d) is not the invocation requested for reset (%d); (active invocation '0' indicates no active invocation)", t.activeInvocationId, resetId)
	}

	t.cancelCurrentInvocation()

	return &ResetResponse{InvocationId: resetId}, nil
}
