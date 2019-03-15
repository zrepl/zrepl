package heartbeatconn

import (
	"fmt"
	"os"
)

var debugEnabled bool = false

func init() {
	if os.Getenv("ZREPL_RPC_DATACONN_HEARTBEATCONN_DEBUG") != "" {
		debugEnabled = true
	}
}

func debug(format string, args ...interface{}) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "rpc/dataconn/heartbeatconn: %s\n", fmt.Sprintf(format, args...))
	}
}
