package trace

import (
	"context"
	"time"

	"github.com/zrepl/zrepl/util/chainlock"
)

type SpanInfo interface {
	StartedAt() time.Time
	EndedAt() time.Time
	TaskAndSpanStack(kind *StackKind) string
}

type Callback struct {
	OnBegin func(ctx context.Context)
	OnEnd   func(ctx context.Context, spanInfo SpanInfo)
}

var callbacks struct {
	mtx chainlock.L
	cs  []Callback
}

func RegisterCallback(c Callback) {
	callbacks.mtx.HoldWhile(func() {
		callbacks.cs = append(callbacks.cs, c)
	})
}

func callbackBeginSpan(ctx context.Context) func(SpanInfo) {
	// capture the current state of callbacks into a local variable
	// this is safe because the slice is append-only and immutable

	// (it is important that a callback registered _after_ callbackBeginSpin is called does not get called on OnEnd)

	var cbs []Callback
	callbacks.mtx.HoldWhile(func() {
		cbs = callbacks.cs
	})
	for _, cb := range cbs {
		if cb.OnBegin != nil {
			cb.OnBegin(ctx)
		}
	}
	return func(spanInfo SpanInfo) {
		for _, cb := range cbs {
			if cb.OnEnd != nil {
				cb.OnEnd(ctx, spanInfo)
			}
		}
	}
}
