package snapper

import (
	"context"

	"github.com/LyingCak3/zrepl/internal/daemon/logging"
	"github.com/LyingCak3/zrepl/internal/logger"
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
