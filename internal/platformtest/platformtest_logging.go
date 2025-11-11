package platformtest

import (
	"context"

	"github.com/LyingCak3/zrepl/internal/daemon/logging"
	"github.com/LyingCak3/zrepl/internal/logger"
)

type Logger = logger.Logger

func GetLog(ctx context.Context) Logger {
	return logging.GetLogger(ctx, logging.SubsysPlatformtest)
}
