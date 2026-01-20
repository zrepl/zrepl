package dataconn

import (
	"fmt"
	"os"
)

//nolint:unused
var debugEnabled bool = false

func init() {
	if os.Getenv("ZREPL_RPC_DATACONN_DEBUG") != "" {
		debugEnabled = true
	}
}

//nolint:unused
func debug(format string, args ...interface{}) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "rpc/dataconn: %s\n", fmt.Sprintf(format, args...))
	}
}
