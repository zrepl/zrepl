package reset

import (
	"context"
	"errors"
)

type contextKey int

const contextKeyReset contextKey = iota

func Wait(ctx context.Context) <-chan struct{} {
	wc, ok := ctx.Value(contextKeyReset).(chan struct{})
	if !ok {
		wc = make(chan struct{})
	}
	return wc
}

type Func func() error

var AlreadyReset = errors.New("already reset")

func Context(ctx context.Context) (context.Context, Func) {
	wc := make(chan struct{})
	wuf := func() error {
		select {
		case wc <- struct{}{}:
			return nil
		default:
			return AlreadyReset
		}
	}
	return context.WithValue(ctx, contextKeyReset, wc), wuf
}
