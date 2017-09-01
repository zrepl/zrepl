package cmd

import (
	"time"

	"github.com/zrepl/zrepl/jobrun"
)

func (p *Pull) JobName() string {
	return p.jobName
}

func (p *Pull) JobDo(log jobrun.Logger) (err error) {
	return jobPull(p, log)
}

func (p *Pull) JobRepeatStrategy() jobrun.RepeatStrategy {
	return p.RepeatStrategy
}

func (a *Autosnap) JobName() string {
	return a.jobName
}

func (a *Autosnap) JobDo(log jobrun.Logger) (err error) {
	return doAutosnap(AutosnapContext{a}, log)
}

func (a *Autosnap) JobRepeatStrategy() jobrun.RepeatStrategy {
	return a.Interval
}

func (p *Prune) JobName() string {
	return p.jobName
}

func (p *Prune) JobDo(log jobrun.Logger) (err error) {
	return doPrune(PruneContext{p, time.Now(), false}, log)
}

func (p *Prune) JobRepeatStrategy() jobrun.RepeatStrategy {
	return p.Repeat
}
