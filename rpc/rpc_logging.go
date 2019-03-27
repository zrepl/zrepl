package rpc

import (
	"context"

	"github.com/zrepl/zrepl/logger"
)

type Logger = logger.Logger

type contextKey int

const (
	contextKeyLoggers contextKey = iota
)

/// All fields must be non-nil
type Loggers struct {
	General Logger
	Control Logger
	Data    Logger
}

func WithLoggers(ctx context.Context, loggers Loggers) context.Context {
	ctx = context.WithValue(ctx, contextKeyLoggers, loggers)
	return ctx
}

func GetLoggersOrPanic(ctx context.Context) Loggers {
	return ctx.Value(contextKeyLoggers).(Loggers)
}
