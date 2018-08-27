package logging

import (
	"fmt"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/logger"
	"strings"
)

type streamrpcLogAdaptor = twoClassLogAdaptor

type twoClassLogAdaptor struct {
	logger.Logger
}

var _ streamrpc.Logger = twoClassLogAdaptor{}

func (a twoClassLogAdaptor) Errorf(fmtStr string, args ...interface{}) {
	const errorSuffix = ": %s"
	if len(args) == 1 {
		if err, ok := args[0].(error); ok && strings.HasSuffix(fmtStr, errorSuffix) {
			msg := strings.TrimSuffix(fmtStr, errorSuffix)
			a.WithError(err).Error(msg)
			return
		}
	}
	a.Logger.Error(fmt.Sprintf(fmtStr, args...))
}

func (a twoClassLogAdaptor) Infof(fmtStr string, args ...interface{}) {
	a.Logger.Info(fmt.Sprintf(fmtStr, args...))
}
