// +build !linux

package zfs

import (
	"os"
	"sync"
)

var zfsPipeCapacityNotSupported sync.Once

func trySetPipeCapacity(p *os.File, capacity int) {
	if debugEnabled {
		zfsPipeCapacityNotSupported.Do(func() {
			debug("trySetPipeCapacity error: OS does not support setting pipe capacity")
		})
	}
}
