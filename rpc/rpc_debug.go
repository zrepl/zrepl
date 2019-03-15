package rpc

import (
	"fmt"
	"os"
)

var debugEnabled bool = false

func init() {
	if os.Getenv("ZREPL_RPC_DEBUG") != "" {
		debugEnabled = true
	}
}

func debug(format string, args ...interface{}) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "rpc: %s\n", fmt.Sprintf(format, args...))
	}
}
