package rpc

import (
	"fmt"
	"os"
)

//nolint:unused
var debugEnabled bool = false

func init() {
	if os.Getenv("ZREPL_RPC_DEBUG") != "" {
		debugEnabled = true
	}
}

//nolint[:deadcode,unused]
func debug(format string, args ...interface{}) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "rpc: %s\n", fmt.Sprintf(format, args...))
	}
}
