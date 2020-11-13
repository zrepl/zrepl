package semaphore

import (
	"context"

	wsemaphore "golang.org/x/sync/semaphore"

	"github.com/zrepl/zrepl/daemon/logging/trace"
)

type S struct {
	ws *wsemaphore.Weighted
}

func New(max int64) *S {
	return &S{wsemaphore.NewWeighted(max)}
}

type AcquireGuard struct {
	s        *S
	released bool
}

// The returned AcquireGuard is not goroutine-safe.
func (s *S) Acquire(ctx context.Context) (*AcquireGuard, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()
	if err := s.ws.Acquire(ctx, 1); err != nil {
		return nil, err
	} else if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &AcquireGuard{s, false}, nil
}

func (g *AcquireGuard) Release() {
	if g == nil || g.released {
		return
	}
	g.released = true
	g.s.ws.Release(1)
}
