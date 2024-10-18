package wakeup

import (
	"context"
	"errors"
)

type contextKey int

const contextKeyWakeup contextKey = iota

func Wait(ctx context.Context) <-chan struct{} {
	wc, ok := ctx.Value(contextKeyWakeup).(chan struct{})
	if !ok {
		wc = make(chan struct{})
	}
	return wc
}

type Func func() error

var AlreadyWokenUp = errors.New("already woken up")

func Context(ctx context.Context) (context.Context, Func) {
	wc := make(chan struct{})
	wuf := func() error {
		select {
		case wc <- struct{}{}:
			return nil
		default:
			return AlreadyWokenUp
		}
	}
	return context.WithValue(ctx, contextKeyWakeup, wc), wuf
}
