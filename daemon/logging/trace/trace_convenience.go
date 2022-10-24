package trace

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// use like this:
//
//	defer WithSpanFromStackUpdateCtx(&existingCtx)()
func WithSpanFromStackUpdateCtx(ctx *context.Context) DoneFunc {
	childSpanCtx, end := WithSpan(*ctx, getMyCallerOrPanic())
	*ctx = childSpanCtx
	return end
}

// derive task name from call stack (caller's name)
func WithTaskFromStack(ctx context.Context) (context.Context, DoneFunc) {
	return WithTask(ctx, getMyCallerOrPanic())
}

// derive task name from call stack (caller's name) and update *ctx
// to point to be the child task ctx
func WithTaskFromStackUpdateCtx(ctx *context.Context) DoneFunc {
	child, end := WithTask(*ctx, getMyCallerOrPanic())
	*ctx = child
	return end
}

// create a task and a span within it in one call
func WithTaskAndSpan(ctx context.Context, task string, span string) (context.Context, DoneFunc) {
	ctx, endTask := WithTask(ctx, task)
	ctx, endSpan := WithSpan(ctx, fmt.Sprintf("%s %s", task, span))
	return ctx, func() {
		endSpan()
		endTask()
	}
}

// create a span during which several child tasks are spawned using the `add` function
//
// IMPORTANT FOR USERS: Caller must ensure that the capturing behavior is correct, the Go linter doesn't catch this.
func WithTaskGroup(ctx context.Context, taskGroup string) (_ context.Context, add func(f func(context.Context)), waitEnd DoneFunc) {
	var wg sync.WaitGroup
	ctx, endSpan := WithSpan(ctx, taskGroup)
	add = func(f func(context.Context)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, endTask := WithTask(ctx, taskGroup)
			defer endTask()
			f(ctx)
		}()
	}
	waitEnd = func() {
		wg.Wait()
		endSpan()
	}
	return ctx, add, waitEnd
}

func getMyCallerOrPanic() string {
	pc, _, _, ok := runtime.Caller(2)
	if !ok {
		panic("cannot get caller")
	}
	details := runtime.FuncForPC(pc)
	if ok && details != nil {
		const prefix = "github.com/zrepl/zrepl"
		return strings.TrimPrefix(strings.TrimPrefix(details.Name(), prefix), "/")
	}
	return ""
}
