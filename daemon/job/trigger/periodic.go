package trigger

import (
	"context"
	"time"
)

type Periodic struct {
	interval time.Duration
}

var _ Trigger = &Periodic{}

func NewPeriodic(interval time.Duration) *Periodic {
	return &Periodic{
		interval: interval,
	}
}

func (p *Periodic) ID() string { return "periodic" }

func (p *Periodic) run(ctx context.Context, signal chan<- struct{}) {
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			signal <- struct{}{}
		case <-ctx.Done():
			return
		}
	}
}
