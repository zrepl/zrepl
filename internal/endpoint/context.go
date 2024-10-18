package endpoint

import (
	"context"

	"github.com/zrepl/zrepl/internal/daemon/logging"
	"github.com/zrepl/zrepl/internal/logger"
)

type contextKey int

const (
	ClientIdentityKey contextKey = iota
)

type Logger = logger.Logger

func getLogger(ctx context.Context) Logger {
	return logging.GetLogger(ctx, logging.SubsysEndpoint)
}
