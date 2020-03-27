package zfscmd

import (
	"context"

	"github.com/zrepl/zrepl/logger"
)

type contextKey int

const (
	contextKeyLogger contextKey = iota
	contextKeyJobID
)

type Logger = logger.Logger

func WithJobID(ctx context.Context, jobID string) context.Context {
	return context.WithValue(ctx, contextKeyJobID, jobID)
}

func getJobIDOrDefault(ctx context.Context, def string) string {
	ret, ok := ctx.Value(contextKeyJobID).(string)
	if !ok {
		return def
	}
	return ret
}

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLogger, log)
}

func getLogger(ctx context.Context) Logger {
	if l, ok := ctx.Value(contextKeyLogger).(Logger); ok {
		return l
	}
	return logger.NewNullLogger()
}
