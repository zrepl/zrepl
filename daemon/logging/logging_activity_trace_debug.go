package logging

import (
	"fmt"
	"os"
)

const chrometraceEnableDebug = false

func chrometraceDebug(format string, args ...interface{}) {
	if !chrometraceEnableDebug {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
