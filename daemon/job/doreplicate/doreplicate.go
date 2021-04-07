package doreplicate

import (
	"context"
	"errors"
)

type contextKey int

const contextKeyReplicate contextKey = iota

func Wait(ctx context.Context) <-chan struct{} {
	wc, ok := ctx.Value(contextKeyReplicate).(chan struct{})
	if !ok {
		wc = make(chan struct{})
	}
	return wc
}

type Func func() error

var AlreadyReplicate = errors.New("Cannot start replication")

func Context(ctx context.Context) (context.Context, Func) {
	wc := make(chan struct{})
	wuf := func() error {
		select {
		case wc <- struct{}{}:
			return nil
		default:
			return AlreadyReplicate
		}
	}
	return context.WithValue(ctx, contextKeyReplicate, wc), wuf
}
