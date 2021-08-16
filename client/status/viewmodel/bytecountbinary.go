package viewmodel

import (
	"fmt"
	"math"
)

func ByteCountBinaryUint(b uint64) string {
	if b > math.MaxInt64 {
		panic(b)
	}
	return ByteCountBinary(int64(b))
}

func ByteCountBinary(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := unit, 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
