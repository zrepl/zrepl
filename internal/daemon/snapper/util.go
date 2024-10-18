package snapper

import (
	"context"

	"github.com/zrepl/zrepl/internal/daemon/logging"
	"github.com/zrepl/zrepl/internal/logger"
)

type Logger = logger.Logger

func getLogger(ctx context.Context) Logger {
	return logging.GetLogger(ctx, logging.SubsysSnapshot)
}

func errOrEmptyString(e error) string {
	if e != nil {
		return e.Error()
	}
	return ""
}
