package snapper

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/hooks"
	"github.com/zrepl/zrepl/daemon/snapper/snapname"
	"github.com/zrepl/zrepl/util/suspendresumesafetimer"
	"github.com/zrepl/zrepl/zfs"
)

func cronFromConfig(fsf zfs.DatasetFilter, in config.SnapshottingCron) (*Cron, error) {

	hooksList, err := hooks.ListFromConfig(&in.Hooks)
	if err != nil {
		return nil, errors.Wrap(err, "hook config error")
	}

	formatter, err := snapname.New(in.Prefix, in.TimestampFormat, in.TimestampLocation)
	if err != nil {
		return nil, errors.Wrap(err, "build snapshot name formatter")
	}

	planArgs := planArgs{
		formatter: formatter,
		hooks:     hooksList,
	}
	return &Cron{config: in, fsf: fsf, planArgs: planArgs}, nil
}

type Cron struct {
	config   config.SnapshottingCron
	fsf      zfs.DatasetFilter
	planArgs planArgs

	mtx sync.RWMutex

	running                 bool
	wakeupTime              time.Time // zero value means uninit
	lastError               error
	lastPlan                *plan
	wakeupWhileRunningCount int
}

func (s *Cron) Run(ctx context.Context, snapshotsTaken chan<- struct{}) {

	for {
		now := time.Now()
		s.mtx.Lock()
		s.wakeupTime = s.config.Cron.Schedule.Next(now)
		s.mtx.Unlock()

		ctxDone := suspendresumesafetimer.SleepUntil(ctx, s.wakeupTime)
		if ctxDone != nil {
			return
		}

		getLogger(ctx).Debug("cron timer fired")
		s.mtx.Lock()
		if s.running {
			getLogger(ctx).Warn("snapshotting triggered according to cron rules but previous snapshotting is not done; not taking a snapshot this time")
			s.wakeupWhileRunningCount++
			s.mtx.Unlock()
			continue
		}
		s.lastError = nil
		s.lastPlan = nil
		s.wakeupWhileRunningCount = 0
		s.running = true
		s.mtx.Unlock()
		go func() {
			err := s.do(ctx)
			s.mtx.Lock()
			s.lastError = err
			s.running = false
			s.mtx.Unlock()

			select {
			case snapshotsTaken <- struct{}{}:
			default:
				if snapshotsTaken != nil {
					getLogger(ctx).Warn("callback channel is full, discarding snapshot update event")
				}
			}
		}()
	}

}

func (s *Cron) do(ctx context.Context) error {
	fss, err := zfs.ZFSListMapping(ctx, s.fsf)
	if err != nil {
		return errors.Wrap(err, "cannot list filesystems")
	}
	p := makePlan(s.planArgs, fss)

	s.mtx.Lock()
	s.lastPlan = p
	s.lastError = nil
	s.mtx.Unlock()

	ok := p.execute(ctx, false)
	if !ok {
		return errors.New("one or more snapshots could not be created, check logs for details")
	} else {
		return nil
	}
}

type CronState string

const (
	CronStateRunning CronState = "running"
	CronStateWaiting CronState = "waiting"
)

type CronReport struct {
	State      CronState
	WakeupTime time.Time
	Errors     []string
	Progress   []*ReportFilesystem
}

func (s *Cron) Report() Report {

	s.mtx.Lock()
	defer s.mtx.Unlock()

	r := CronReport{}

	r.WakeupTime = s.wakeupTime

	if s.running {
		r.State = CronStateRunning
	} else {
		r.State = CronStateWaiting
	}

	if s.lastError != nil {
		r.Errors = append(r.Errors, s.lastError.Error())
	}
	if s.wakeupWhileRunningCount > 0 {
		r.Errors = append(r.Errors, fmt.Sprintf("cron frequency is too high; snapshots were not taken %d times", s.wakeupWhileRunningCount))
	}

	r.Progress = nil
	if s.lastPlan != nil {
		r.Progress = s.lastPlan.report()
	}

	return Report{Type: TypeCron, Cron: &r}
}
