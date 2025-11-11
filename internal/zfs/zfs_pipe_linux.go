package zfs

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LyingCak3/zrepl/internal/util/envconst"
)

func getPipeCapacityHint(envvar string) int {
	var capacity int64 = 1 << 25

	// Work around a race condition in Linux >= 5.8 related to pipe resizing.
	// https://github.com/LyingCak3/zrepl/issues/424#issuecomment-800370928
	// https://bugzilla.kernel.org/show_bug.cgi?id=212295
	if _, err := os.Stat("/proc/sys/fs/pipe-max-size"); err == nil {
		if dat, err := os.ReadFile("/proc/sys/fs/pipe-max-size"); err == nil {
			if capacity, err = strconv.ParseInt(strings.TrimSpace(string(dat)), 10, 64); err != nil {
				capacity = 1 << 25
			}
		}
	}

	return int(envconst.Int64(envvar, capacity))
}

func trySetPipeCapacity(p *os.File, capacity int) {
	res, err := unix.FcntlInt(p.Fd(), unix.F_SETPIPE_SZ, capacity)
	if err != nil {
		err = fmt.Errorf("cannot set pipe capacity to %v", capacity)
	} else if res == -1 {
		err = errors.New("cannot set pipe capacity: fcntl returned -1")
	}
	if debugEnabled && err != nil {
		debug("trySetPipeCapacity error: %s\n", err)
	}
}
