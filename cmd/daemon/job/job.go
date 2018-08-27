package job

import (
	"context"
	"github.com/zrepl/zrepl/logger"
)

type Logger = logger.Logger

type contextKey int

const (
	contextKeyLog contextKey = iota
	contextKeyWakeup
)

func GetLogger(ctx context.Context) Logger {
	if l, ok := ctx.Value(contextKeyLog).(Logger); ok {
		return l
	}
	return logger.NewNullLogger()
}

func WithLogger(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, contextKeyLog, l)
}

func WithWakeup(ctx context.Context) (context.Context, WakeupChan) {
	wc := make(chan struct{}, 1)
	return context.WithValue(ctx, contextKeyWakeup, wc), wc
}

type Job interface {
	Name() string
	Run(ctx context.Context)
	Status() interface{}
}

type WakeupChan <-chan struct{}

func WaitWakeup(ctx context.Context) WakeupChan {
	wc, ok := ctx.Value(contextKeyWakeup).(WakeupChan)
	if !ok {
		wc = make(chan struct{})
	}
	return wc
}
