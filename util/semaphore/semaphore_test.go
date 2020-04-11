package semaphore

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/daemon/logging"
)

func TestSemaphore(t *testing.T) {
	const numGoroutines = 10
	const concurrentSemaphore = 5
	const sleepTime = 1 * time.Second

	begin := time.Now()

	sem := New(concurrentSemaphore)

	var acquisitions struct {
		beforeT, afterT uint32
	}

	ctx := context.Background()
	defer logging.WithTaskFromStackUpdateCtx(&ctx)()

	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			ctx, end := logging.WithTaskFromStack(ctx)
			defer end()
			defer wg.Done()
			res, err := sem.Acquire(ctx)
			require.NoError(t, err)
			defer res.Release()
			if time.Since(begin) > sleepTime {
				atomic.AddUint32(&acquisitions.beforeT, 1)
			} else {
				atomic.AddUint32(&acquisitions.afterT, 1)
			}
			time.Sleep(sleepTime)
		}()
	}

	wg.Wait()

	assert.True(t, acquisitions.beforeT == concurrentSemaphore)
	assert.True(t, acquisitions.afterT == numGoroutines-concurrentSemaphore)

}
