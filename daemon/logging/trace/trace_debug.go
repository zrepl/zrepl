package trace

import (
	"fmt"
	"os"
)

const debugEnabled = false

func debug(format string, args ...interface{}) {
	if !debugEnabled {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
