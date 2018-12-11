package endpoint

import (
	"context"
	"github.com/zrepl/zrepl/logger"
)

type contextKey int

const (
	contextKeyLogger contextKey = iota
	ClientIdentityKey
)

type Logger = logger.Logger

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLogger, log)
}

func getLogger(ctx context.Context) Logger {
	if l, ok := ctx.Value(contextKeyLogger).(Logger); ok {
		return l
	}
	return logger.NewNullLogger()
}
