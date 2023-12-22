package trigger

import (
	"context"

	"github.com/robfig/cron/v3"
)

type Cron struct {
	spec cron.Schedule
}

var _ Trigger = &Cron{}

func NewCron(spec cron.Schedule) *Cron {
	return &Cron{spec: spec}
}

func (t *Cron) ID() string { return "cron" }

func (t *Cron) run(ctx context.Context, signal chan<- struct{}) {
	panic("unimpl: extract from cron snapper")
}
