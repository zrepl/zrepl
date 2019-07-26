package circlog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNextPow2Int(t *testing.T) {
	require.Equal(t, 512, nextPow2Int(512), "a power of 2 should round to itself")
	require.Equal(t, 1024, nextPow2Int(513), "should round up to the next power of 2")
	require.PanicsWithValue(t, "can only round up positive integers", func() { nextPow2Int(0) }, "unimplemented: zero is not positive; corner case")
	require.PanicsWithValue(t, "can only round up positive integers", func() { nextPow2Int(-1) }, "unimplemented: cannot round up negative numbers")
	maxInt := int((^uint(0)) >> 1)
	require.PanicsWithValue(t, "rounded to larger than int()", func() { nextPow2Int(maxInt - 1) }, "cannot round to a number bigger than the int type")
}
