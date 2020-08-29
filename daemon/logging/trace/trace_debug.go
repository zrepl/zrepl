package trace

import (
	"fmt"
	"os"

	"github.com/zrepl/zrepl/util/envconst"
)

var debugEnabled = envconst.Bool("ZREPL_TRACE_DEBUG_ENABLED", false)

func debug(format string, args ...interface{}) {
	if !debugEnabled {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
