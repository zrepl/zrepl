package platformtest

import (
	"context"

	"github.com/zrepl/zrepl/internal/daemon/logging"
	"github.com/zrepl/zrepl/internal/logger"
)

type Logger = logger.Logger

func GetLog(ctx context.Context) Logger {
	return logging.GetLogger(ctx, logging.SubsysPlatformtest)
}
