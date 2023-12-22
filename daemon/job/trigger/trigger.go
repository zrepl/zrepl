package trigger

import (
	"context"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/logging/trace"
)

type Triggers struct {
	spawned  bool
	triggers []*Trigger
}

type Trigger struct {
	id     string
	signal chan struct{}
}

func FromConfig([]*config.ReplicationTriggerEnum) (*Triggers, error) {
	panic("unimpl")
	return &Triggers{
		spawned: false,
	}, nil
}

func Empty() *Triggers {
	panic("unimpl")
}

func New(id string) *Trigger {
	return &Trigger{
		id:     id,
		signal: make(chan struct{}),
	}
}

func (t *Trigger) ID() string {
	return t.id
}

func (t *Trigger) Fire() error {
	panic("unimpl")
}

func (t *Triggers) Spawn(ctx context.Context, additionalTriggers []*Trigger) (chan *Trigger, trace.DoneFunc) {
	if t.spawned {
		panic("must only spawn once")
	}
	t.spawned = true
	t.triggers = append(t.triggers, additionalTriggers...)
	childCtx, endTask := trace.WithTask(ctx, "triggers")
	sink := make(chan *Trigger)
	go t.task(childCtx, sink)
	return sink, endTask
}

type triggering struct {
	trigger *Trigger
	handled chan struct{}
}

func (t *Triggers) task(ctx context.Context, sink chan *Trigger) {
	triggered := make(chan triggering, len(t.triggers))
	for _, t := range t.triggers {
		t := t
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.signal:
					handled := make(chan struct{})
					select {
					case triggered <- triggering{trigger: t, handled: handled}:
					default:
						panic("this funtion ensures that there's always room in the channel")
					}
					select {
					case <-handled:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case triggering := <-triggered:
			select {
			case sink <- triggering.trigger:
			default:
				getLogger(ctx).
					WithField("trigger_id", triggering.trigger.id).
					Warn("dropping triggering because job is busy")
			}
			close(triggering.handled)
		}
	}
}
