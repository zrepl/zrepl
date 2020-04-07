package driver

import (
	"fmt"
	"os"
)

var debugEnabled bool = false

func init() {
	if os.Getenv("ZREPL_REPLICATION_DRIVER_DEBUG") != "" {
		debugEnabled = true
	}
}

//nolint[:deadcode,unused]
func debug(format string, args ...interface{}) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "repl: driver: %s\n", fmt.Sprintf(format, args...))
	}
}

type debugFunc func(format string, args ...interface{})

//nolint[:deadcode,unused]
func debugPrefix(prefixFormat string, prefixFormatArgs ...interface{}) debugFunc {
	prefix := fmt.Sprintf(prefixFormat, prefixFormatArgs...)
	return func(format string, args ...interface{}) {
		debug("%s: %s", prefix, fmt.Sprintf(format, args...))
	}
}
