package driver

import (
	"context"

	"github.com/zrepl/zrepl/logger"
)

type Logger = logger.Logger

type contextKey int

const contextKeyLogger contextKey = iota + 1

func getLog(ctx context.Context) Logger {
	l, ok := ctx.Value(contextKeyLogger).(Logger)
	if !ok {
		l = logger.NewNullLogger()
	}
	return l
}

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLogger, log)
}
