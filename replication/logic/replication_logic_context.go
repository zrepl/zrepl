package logic

import (
	"context"

	"github.com/zrepl/zrepl/logger"
)

type contextKey int

const (
	contextKeyLog contextKey = iota
)

type Logger = logger.Logger

func WithLogger(ctx context.Context, l Logger) context.Context {
	ctx = context.WithValue(ctx, contextKeyLog, l)
	return ctx
}

func getLogger(ctx context.Context) Logger {
	l, ok := ctx.Value(contextKeyLog).(Logger)
	if !ok {
		l = logger.NewNullLogger()
	}
	return l
}
