package trace

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/willf/bitset"
)

func TestBitsetFeaturesForUniqueConcurrentTaskNamer(t *testing.T) {
	var b bitset.BitSet
	require.Equal(t, uint(0), b.Len())
	require.Equal(t, uint(0), b.Count())

	b.Set(0)
	require.Equal(t, uint(1), b.Len())
	require.Equal(t, uint(1), b.Count())

	b.Set(8)
	require.Equal(t, uint(9), b.Len())
	require.Equal(t, uint(2), b.Count())

	b.Set(1)
	require.Equal(t, uint(9), b.Len())
	require.Equal(t, uint(3), b.Count())
}

func TestUniqueConcurrentTaskNamer(t *testing.T) {
	namer := newUniqueTaskNamer(nil)

	var wg sync.WaitGroup

	const N = 8128
	const Q = 23

	var fails uint32
	var m sync.Map
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("%d", i/Q)
			uniqueName, done := namer.UniqueConcurrentTaskName(name)
			act, _ := m.LoadOrStore(uniqueName, i)
			if act.(int) != i {
				atomic.AddUint32(&fails, 1)
			}
			m.Delete(uniqueName)
			done()
		}(i)
	}
	wg.Wait()

	require.Equal(t, uint32(0), fails)
}
