package doprune

import (
	"context"
	"errors"
)

type contextKey int

const contextKeyDoprune contextKey = iota

func Wait(ctx context.Context) <-chan struct{} {
	wc, ok := ctx.Value(contextKeyDoprune).(chan struct{})
	if !ok {
		wc = make(chan struct{})
	}
	return wc
}

type Func func() error

var AlreadyDoprune = errors.New("Cannot start pruning")

func Context(ctx context.Context) (context.Context, Func) {
	wc := make(chan struct{})
	wuf := func() error {
		select {
		case wc <- struct{}{}:
			return nil
		default:
			return AlreadyDoprune
		}
	}
	return context.WithValue(ctx, contextKeyDoprune, wc), wuf
}
