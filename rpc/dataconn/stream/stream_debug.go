package stream

import (
	"fmt"
	"os"
)

var debugEnabled bool = false

func init() {
	if os.Getenv("ZREPL_RPC_DATACONN_STREAM_DEBUG") != "" {
		debugEnabled = true
	}
}

//nolint[:deadcode,unused]
func debug(format string, args ...interface{}) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "rpc/dataconn/stream: %s\n", fmt.Sprintf(format, args...))
	}
}
