package trigger

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBasics(t *testing.T) {

	var wg sync.WaitGroup
	defer wg.Wait()

	tr := New()

	triggered := make(chan int)
	waitForTriggerError := make(chan error)
	waitForResetCallToBeMadeByMainGoroutine := make(chan struct{})
	postResetAssertionsDone := make(chan struct{})

	taskCtx := context.Background()
	taskCtx, cancelTaskCtx := context.WithCancel(taskCtx)

	wg.Add(1)
	go func() {
		defer wg.Done()

		taskCtx := context.WithValue(taskCtx, "mykey", "myvalue")

		triggers := 0

	outer:
		for {
			invocationCtx, err := tr.WaitForTrigger(taskCtx)
			if err != nil {
				waitForTriggerError <- err
				return
			}
			require.Equal(t, invocationCtx.Value("mykey"), "myvalue")

			triggers++
			triggered <- triggers

			switch triggers {
			case 1:
				continue outer
			case 2:
				<-waitForResetCallToBeMadeByMainGoroutine
				require.Equal(t, context.Canceled, invocationCtx.Err(), "Reset() cancels invocation context")
				require.Nil(t, taskCtx.Err(), "Reset() does not cancel task context")
				close(postResetAssertionsDone)
			}

		}

	}()

	t.Logf("trigger 1")
	_, err := tr.Trigger()
	require.NoError(t, err)
	v := <-triggered
	require.Equal(t, 1, v)

	t.Logf("trigger 2")
	triggerResponse, err := tr.Trigger()
	require.NoError(t, err)
	v = <-triggered
	require.Equal(t, 2, v)

	t.Logf("reset")
	resetResponse, err := tr.Reset(ResetRequest{InvocationId: triggerResponse.InvocationId})
	require.NoError(t, err)
	t.Logf("reset response: %#v", resetResponse)
	close(waitForResetCallToBeMadeByMainGoroutine)
	<-postResetAssertionsDone

	t.Logf("cancel the context")
	cancelTaskCtx()
	wfte := <-waitForTriggerError
	require.Equal(t, taskCtx.Err(), wfte)

}
