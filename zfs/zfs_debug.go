package zfs

import (
	"fmt"
	"os"
)

var debugEnabled bool = false

func init() {
	if os.Getenv("ZREPL_ZFS_DEBUG") != "" {
		debugEnabled = true
	}
}

//nolint[:deadcode,unused]
func debug(format string, args ...interface{}) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "zfs: %s\n", fmt.Sprintf(format, args...))
	}
}
