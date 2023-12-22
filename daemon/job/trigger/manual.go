package trigger

import "context"

type Manual struct {
	id     string
	signal chan<- struct{}
}

var _ Trigger = &Manual{}

func NewManual(id string) *Manual {
	return &Manual{
		id:     id,
		signal: nil,
	}
}

func (t *Manual) ID() string {
	return t.id
}

func (t *Manual) run(ctx context.Context, signal chan<- struct{}) {
	if t.signal != nil {
		panic("run must only be called once")
	}
	t.signal = signal
}

// Panics if called before the trigger has been spanwed as part of a `Triggers`.
func (t *Manual) Fire() {
	t.signal <- struct{}{}
}
