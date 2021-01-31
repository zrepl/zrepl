package doreplication

import (
	"context"
	"errors"
)

type contextKey int

const contextKeyReplication contextKey = iota

func Wait(ctx context.Context) <-chan struct{} {
	wc, ok := ctx.Value(contextKeyReplication).(chan struct{})
	if !ok {
		wc = make(chan struct{})
	}
	return wc
}

type Func func() error

var AlreadyReplicating = errors.New("already replicating")

func Context(ctx context.Context) (context.Context, Func) {
	wc := make(chan struct{})
	wuf := func() error {
		select {
		case wc <- struct{}{}:
			return nil
		default:
			return AlreadyReplicating
		}
	}
	return context.WithValue(ctx, contextKeyReplication, wc), wuf
}
