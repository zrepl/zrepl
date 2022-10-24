package rpc

import (
	"context"

	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/logger"
)

type Logger = logger.Logger

// All fields must be non-nil
type Loggers struct {
	General Logger
	Control Logger
	Data    Logger
}

func GetLoggersOrPanic(ctx context.Context) Loggers {
	return Loggers{
		General: logging.GetLogger(ctx, logging.SubsysRPC),
		Control: logging.GetLogger(ctx, logging.SubsysRPCControl),
		Data:    logging.GetLogger(ctx, logging.SubsysRPCData),
	}
}
