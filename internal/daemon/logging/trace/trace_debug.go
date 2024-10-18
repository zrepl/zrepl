package trace

import (
	"fmt"
	"os"

	"github.com/zrepl/zrepl/internal/util/envconst"
)

const debugEnabledEnvVar = "ZREPL_TRACE_DEBUG_ENABLED"

var debugEnabled = envconst.Bool(debugEnabledEnvVar, false)

func debug(format string, args ...interface{}) {
	if !debugEnabled {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
