package base2bufpool

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPoolAllocBehavior(t *testing.T) {

	type testcase struct {
		poolMinShift, poolMaxShift uint
		behavior                   NoFitBehavior
		get                        uint
		expShiftBufLen             int64 // -1 if panic expected
	}

	tcs := []testcase{
		{
			15, 20, Allocate,
			1 << 14, 1 << 14,
		},
		{
			15, 20, Allocate,
			1 << 22, 1 << 22,
		},
		{
			15, 20, Panic,
			1 << 16, 1 << 16,
		},
		{
			15, 20, Panic,
			1 << 14, -1,
		},
		{
			15, 20, Panic,
			1 << 22, -1,
		},
		{
			15, 20, Panic,
			(1 << 15) + 23, 1 << 16,
		},
		{
			15, 20, Panic,
			0, -1, // yep, 0 always works, even
		},
		{
			15, 20, Allocate,
			0, 0,
		},
		{
			15, 20, AllocateSmaller,
			1 << 14, 1 << 14,
		},
		{
			15, 20, AllocateSmaller,
			1 << 22, -1,
		},
	}

	for i := range tcs {
		tc := tcs[i]
		t.Run(fmt.Sprintf("[%d,%d] behav=%s Get(%d) exp=%d", tc.poolMinShift, tc.poolMaxShift, tc.behavior, tc.get, tc.expShiftBufLen), func(t *testing.T) {
			pool := New(tc.poolMinShift, tc.poolMaxShift, tc.behavior)
			if tc.expShiftBufLen == -1 {
				assert.Panics(t, func() {
					pool.Get(tc.get)
				})
				return
			}
			buf := pool.Get(tc.get)
			assert.True(t, uint(len(buf.Bytes())) == tc.get)
			assert.True(t, int64(len(buf.shiftBuf)) == tc.expShiftBufLen)
		})
	}
}

func TestFittingShift(t *testing.T) {
	assert.Equal(t, uint(16), fittingShift(1+1<<15))
	assert.Equal(t, uint(15), fittingShift(1<<15))
}

func TestFreeFromPoolRangeDoesNotPanic(t *testing.T) {
	pool := New(15, 20, Allocate)
	buf := pool.Get(1 << 16)
	assert.NotPanics(t, func() {
		buf.Free()
	})
}

func TestFreeFromOutOfPoolRangeDoesNotPanic(t *testing.T) {
	pool := New(15, 20, Allocate)
	buf := pool.Get(1 << 23)
	assert.NotPanics(t, func() {
		buf.Free()
	})
}
