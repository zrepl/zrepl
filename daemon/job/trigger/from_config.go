package trigger

import (
	"fmt"

	"github.com/zrepl/zrepl/config"
)

func FromConfig(in []*config.ReplicationTriggerEnum) (*Triggers, error) {
	triggers := make([]Trigger, len(in))
	for i, e := range in {
		var t Trigger = nil
		switch te := e.Ret.(type) {
		case *config.ReplicationTriggerManual:
			// not a trigger
			t = NewManual("manual")
		case *config.ReplicationTriggerPeriodic:
			t = NewPeriodic(te.Interval.Duration())
		case *config.ReplicationTriggerCron:
			t = NewCron(te.Cron.Schedule)
		default:
			return nil, fmt.Errorf("unknown trigger type %T", te)
		}
		triggers[i] = t
	}
	return &Triggers{
		spawned:  false,
		triggers: triggers,
	}, nil
}
