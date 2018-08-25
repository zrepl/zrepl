package replication

import (
	"context"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/replication/fsrep"
)

type contextKey int

const (
	contextKeyLog contextKey = iota
)

type Logger = logger.Logger

func WithLogger(ctx context.Context, l Logger) context.Context {
	ctx = context.WithValue(ctx, contextKeyLog, l)
	ctx = fsrep.WithLogger(ctx, l)
	return ctx
}

func getLogger(ctx context.Context) Logger {
	l, ok := ctx.Value(contextKeyLog).(Logger)
	if !ok {
		l = logger.NewNullLogger()
	}
	return l
}
