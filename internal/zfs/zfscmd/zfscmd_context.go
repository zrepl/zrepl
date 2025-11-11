package zfscmd

import (
	"context"

	"github.com/LyingCak3/zrepl/internal/daemon/logging"
	"github.com/LyingCak3/zrepl/internal/logger"
)

type contextKey int

const (
	contextKeyJobID contextKey = 1 + iota
)

type Logger = logger.Logger

func WithJobID(ctx context.Context, jobID string) context.Context {
	return context.WithValue(ctx, contextKeyJobID, jobID)
}

func GetJobIDOrDefault(ctx context.Context, def string) string {
	return getJobIDOrDefault(ctx, def)
}

func getJobIDOrDefault(ctx context.Context, def string) string {
	ret, ok := ctx.Value(contextKeyJobID).(string)
	if !ok {
		return def
	}
	return ret
}

func getLogger(ctx context.Context) Logger {
	return logging.GetLogger(ctx, logging.SubsysZFSCmd)
}
