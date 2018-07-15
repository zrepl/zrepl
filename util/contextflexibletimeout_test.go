package util

import (
	"testing"
	"context"
	"time"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/assert"
)

func TestContextWithOptionalDeadline(t *testing.T) {

	ctx := context.Background()
	cctx, enforceDeadline := ContextWithOptionalDeadline(ctx)

	begin := time.Now()
	var receivedCancellation time.Time
	var cancellationError error
	go func() {
		select {
		case <- cctx.Done():
			receivedCancellation = time.Now()
			cancellationError = cctx.Err()
		case <- time.After(600*time.Millisecond):
			t.Fatalf("should have been cancelled by deadline")
		}
	}()
	time.Sleep(100*time.Millisecond)
	if !receivedCancellation.IsZero() {
		t.Fatalf("no enforcement means no cancellation")
	}
	require.Nil(t, cctx.Err(), "no error while not cancelled")
	dl, ok := cctx.Deadline()
	require.False(t, ok)
	require.Zero(t, dl)
	enforceDeadline(begin.Add(200*time.Millisecond))
	// second call must be ignored, i.e. we expect the deadline to be at begin+200ms, not begin+400ms
	enforceDeadline(begin.Add(400*time.Millisecond))

	time.Sleep(300*time.Millisecond) // 100ms margin for scheduler
	if receivedCancellation.Sub(begin) > 250*time.Millisecond {
		t.Fatalf("cancellation is beyond acceptable scheduler latency")
	}
	require.Equal(t, context.DeadlineExceeded, cancellationError)
}

func TestContextWithOptionalDeadlineNegativeDeadline(t *testing.T) {
	ctx := context.Background()
	cctx, enforceDeadline := ContextWithOptionalDeadline(ctx)
	enforceDeadline(time.Now().Add(-10*time.Second))
	select {
	case <-cctx.Done():
	default:
		t.FailNow()
	}
}

func TestContextWithOptionalDeadlineParentCancellation(t *testing.T) {

	pctx, cancel := context.WithCancel(context.Background())
	cctx, enforceDeadline := ContextWithOptionalDeadline(pctx)

	// 0 ms
	start := time.Now()
	enforceDeadline(start.Add(400*time.Millisecond))
	time.Sleep(100*time.Millisecond)
	cancel() // cancel @ ~100ms
	time.Sleep(100*time.Millisecond) // give 100ms time to propagate cancel
	// @ ~200ms
	select {
	case <-cctx.Done():
		assert.True(t, time.Now().Before(start.Add(300*time.Millisecond)))
		assert.Equal(t, context.Canceled, cctx.Err())
	default:
		t.FailNow()
	}

}

func TestContextWithOptionalDeadlineValue(t *testing.T) {
	pctx := context.WithValue(context.Background(), "key", "value")
	cctx, _ := ContextWithOptionalDeadline(pctx)
	assert.Equal(t, "value", cctx.Value("key"))
}