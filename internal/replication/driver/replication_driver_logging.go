package driver

import (
	"context"

	"github.com/LyingCak3/zrepl/internal/daemon/logging"
	"github.com/LyingCak3/zrepl/internal/logger"
)

func getLog(ctx context.Context) logger.Logger {
	return logging.GetLogger(ctx, logging.SubsysReplication)
}
