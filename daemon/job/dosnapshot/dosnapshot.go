package dosnapshot

import (
	"context"
	"errors"
)

type contextKey int

const contextKeyDosnapshot contextKey = iota

func Wait(ctx context.Context) <-chan struct{} {
	wc, ok := ctx.Value(contextKeyDosnapshot).(chan struct{})
	if !ok {
		wc = make(chan struct{})
	}
	return wc
}

type Func func() error

var AlreadyDosnapshot = errors.New("Cannot start snapshotting")

func Context(ctx context.Context) (context.Context, Func) {
	wc := make(chan struct{})
	wuf := func() error {
		select {
		case wc <- struct{}{}:
			return nil
		default:
			return AlreadyDosnapshot
		}
	}
	return context.WithValue(ctx, contextKeyDosnapshot, wc), wuf
}
