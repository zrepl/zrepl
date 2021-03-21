// +build !linux

package zfs

import (
	"os"
	"sync"
)

func getPipeCapacityHint(envvar string) int {
	return 0 // not supported
}

var zfsPipeCapacityNotSupported sync.Once

func trySetPipeCapacity(p *os.File, capacity int) {
	if debugEnabled && capacity != 0 {
		zfsPipeCapacityNotSupported.Do(func() {
			debug("trySetPipeCapacity error: OS does not support setting pipe capacity")
		})
	}
}
