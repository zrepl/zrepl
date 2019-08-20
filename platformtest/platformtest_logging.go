package platformtest

import (
	"context"

	"github.com/zrepl/zrepl/logger"
)

type Logger = logger.Logger

type contextKey int

const (
	contextKeyLogger contextKey = iota
)

func WithLogger(ctx context.Context, logger Logger) context.Context {
	ctx = context.WithValue(ctx, contextKeyLogger, logger)
	return ctx
}

func getLog(ctx context.Context) Logger {
	return ctx.Value(contextKeyLogger).(Logger)
}
