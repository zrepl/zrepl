package logic

import (
	"context"

	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/logger"
)

func getLogger(ctx context.Context) logger.Logger {
	return logging.GetLogger(ctx, logging.SubsysReplication)
}
