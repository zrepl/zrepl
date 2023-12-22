package trigger

import (
	"context"
	"github.com/zrepl/zrepl/daemon/logging/trace"
)

type Triggers struct {
	spawned  bool
	triggers []Trigger
}

type Trigger interface {
	ID() string
	run(context.Context, chan<- struct{})
}

func Empty() *Triggers {
	return &Triggers{
		spawned:  false,
		triggers: nil,
	}
}

func (t *Triggers) Spawn(ctx context.Context, additionalTriggers []Trigger) (chan Trigger, trace.DoneFunc) {
	if t.spawned {
		panic("must only spawn once")
	}
	t.spawned = true
	t.triggers = append(t.triggers, additionalTriggers...)
	sink := make(chan Trigger)
	endTask := t.spawn(ctx, sink)
	return sink, endTask
}

type triggering struct {
	trigger Trigger
	handled chan struct{}
}

func (t *Triggers) spawn(ctx context.Context, sink chan Trigger) trace.DoneFunc {
	ctx, endTask := trace.WithTask(ctx, "triggers")
	ctx, add, wait := trace.WithTaskGroup(ctx, "trigger-tasks")
	triggered := make(chan triggering, len(t.triggers))
	for _, t := range t.triggers {
		t := t
		signal := make(chan struct{})
		go add(func(ctx context.Context) {
			t.run(ctx, signal)
		})
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-signal:
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
	go func() {
		defer wait()
		for {
			select {
			case <-ctx.Done():
				return
			case triggering := <-triggered:
				select {
				case sink <- triggering.trigger:
				default:
					getLogger(ctx).
						WithField("trigger_id", triggering.trigger.ID()).
						Warn("dropping triggering because job is busy")
				}
				close(triggering.handled)
			}
		}
	}()
	return endTask
}
