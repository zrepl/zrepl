package logic

import (
	"context"

	"github.com/zrepl/zrepl/internal/daemon/logging"
	"github.com/zrepl/zrepl/internal/logger"
)

func getLogger(ctx context.Context) logger.Logger {
	return logging.GetLogger(ctx, logging.SubsysReplication)
}
