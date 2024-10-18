package trace

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetCallerOrPanic(t *testing.T) {
	withStackFromCtxMock := func() string {
		return getMyCallerOrPanic()
	}
	ret := withStackFromCtxMock()
	// zrepl prefix is stripped
	assert.Equal(t, "internal/daemon/logging/trace.TestGetCallerOrPanic", ret)
}

func TestWithTaskGroupRunTasksConcurrently(t *testing.T) {

	// spawn a task group where each task waits for the other to start
	// => without concurrency, they would hang

	rootCtx, endRoot := WithTaskFromStack(context.Background())
	defer endRoot()

	_, add, waitEnd := WithTaskGroup(rootCtx, "test-task-group")

	schedulerTimeout := 2 * time.Second
	timeout := time.After(schedulerTimeout)
	var hadTimeout uint32
	started0, started1 := make(chan struct{}), make(chan struct{})
	for i := 0; i < 2; i++ {
		i := i // capture by copy
		add(func(ctx context.Context) {
			switch i {
			case 0:
				close(started0)
				select {
				case <-started1:
				case <-timeout:
					atomic.AddUint32(&hadTimeout, 1)
				}
			case 1:
				close(started1)
				select {
				case <-started0:
				case <-timeout:
					atomic.AddUint32(&hadTimeout, 1)
				}
			default:
				panic("unreachable")
			}
		})
	}

	waitEnd()
	assert.Zero(t, hadTimeout, "either bad impl or scheduler timeout (which is %v)", schedulerTimeout)

}
