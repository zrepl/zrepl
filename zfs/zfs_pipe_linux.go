package zfs

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

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
