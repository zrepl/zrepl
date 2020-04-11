package platformtest

import (
	"context"

	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/logger"
)

type Logger = logger.Logger

func GetLog(ctx context.Context) Logger {
	return logging.GetLogger(ctx, logging.SubsysPlatformtest)
}
