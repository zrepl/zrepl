package driver

import (
	"context"

	"github.com/zrepl/zrepl/logger"
)

type Logger = logger.Logger

type contexKey int

const contexKeyLogger contexKey = iota + 1

func getLog(ctx context.Context) Logger {
	l, ok := ctx.Value(contexKeyLogger).(Logger)
	if !ok {
		l = logger.NewNullLogger()
	}
	return l
}

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contexKeyLogger, log)
}
