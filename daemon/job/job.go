package job

import (
	"context"
	"errors"
	"github.com/prometheus/client_golang/prometheus"
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

type WakeupFunc func() error

var AlreadyWokenUp = errors.New("already woken up")

func WithWakeup(ctx context.Context) (context.Context, WakeupFunc) {
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

type Job interface {
	Name() string
	Run(ctx context.Context)
	Status() interface{}
	RegisterMetrics(registerer prometheus.Registerer)
}

func WaitWakeup(ctx context.Context) <-chan struct{} {
	wc, ok := ctx.Value(contextKeyWakeup).(chan struct{})
	if !ok {
		wc = make(chan struct{})
	}
	return wc
}

