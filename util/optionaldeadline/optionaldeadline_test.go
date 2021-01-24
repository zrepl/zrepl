package optionaldeadline

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/util/chainlock"
)

func TestContextWithOptionalDeadline(t *testing.T) {

	ctx := context.Background()
	cctx, enforceDeadline := ContextWithOptionalDeadline(ctx)

	begin := time.Now()
	var checker struct {
		receivedCancellation time.Time
		cancellationError    error
		timeout              bool
		mtx                  chainlock.L
	}
	go func() {
		select {
		case <-cctx.Done():
			defer checker.mtx.Lock().Unlock()
			checker.receivedCancellation = time.Now()
			checker.cancellationError = cctx.Err()
		case <-time.After(600 * time.Millisecond):
			defer checker.mtx.Lock().Unlock()
			checker.timeout = true
		}
	}()
	defer checker.mtx.Lock().Unlock()
	checker.mtx.DropWhile(func() {
		time.Sleep(100 * time.Millisecond)
	})
	if !checker.receivedCancellation.IsZero() {
		t.Fatalf("no enforcement means no cancellation")
	}
	require.Nil(t, cctx.Err(), "no error while not cancelled")
	dl, ok := cctx.Deadline()
	require.False(t, ok)
	require.Zero(t, dl)
	enforceDeadline(begin.Add(200 * time.Millisecond))
	// second call must be ignored, i.e. we expect the deadline to be at begin+200ms, not begin+400ms
	enforceDeadline(begin.Add(400 * time.Millisecond))

	checker.mtx.DropWhile(func() {
		time.Sleep(300 * time.Millisecond) // 100ms margin for scheduler
	})
	assert.False(t, checker.timeout, "test timeout")
	receivedCancellationAfter := checker.receivedCancellation.Sub(begin)
	if receivedCancellationAfter > 250*time.Millisecond {
		t.Fatalf("cancellation is beyond acceptable scheduler latency: %s", receivedCancellationAfter)
	}
	require.Equal(t, context.DeadlineExceeded, checker.cancellationError)
}

func TestContextWithOptionalDeadlineNegativeDeadline(t *testing.T) {
	ctx := context.Background()
	cctx, enforceDeadline := ContextWithOptionalDeadline(ctx)
	enforceDeadline(time.Now().Add(-10 * time.Second))
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
	enforceDeadline(start.Add(400 * time.Millisecond))
	time.Sleep(100 * time.Millisecond)
	cancel()                           // cancel @ ~100ms
	time.Sleep(100 * time.Millisecond) // give 100ms time to propagate cancel
	// @ ~200ms
	select {
	case <-cctx.Done():
		assert.True(t, time.Now().Before(start.Add(300*time.Millisecond)))
		assert.Equal(t, context.Canceled, cctx.Err())
	default:
		t.FailNow()
	}

}

type testContextKey string

const testContextKeyKey testContextKey = "key"

func TestContextWithOptionalDeadlineValue(t *testing.T) {
	pctx := context.WithValue(context.Background(), testContextKeyKey, "value")
	cctx, _ := ContextWithOptionalDeadline(pctx)
	assert.Equal(t, "value", cctx.Value(testContextKeyKey))
}
