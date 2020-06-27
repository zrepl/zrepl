package snapper

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/daemon/logging/trace"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/hooks"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/zfs"
)

//go:generate stringer -type=SnapState
type SnapState uint

const (
	SnapPending SnapState = 1 << iota
	SnapStarted
	SnapDone
	SnapError
)

// All fields protected by Snapper.mtx
type snapProgress struct {
	state SnapState

	// SnapStarted, SnapDone, SnapError
	name     string
	startAt  time.Time
	hookPlan *hooks.Plan

	// SnapDone
	doneAt time.Time

	// SnapErr TODO disambiguate state
	runResults hooks.PlanReport
}

type args struct {
	ctx            context.Context
	prefix         string
	interval       time.Duration
	fsf            zfs.DatasetFilter
	snapshotsTaken chan<- struct{}
	hooks          *hooks.List
	dryRun         bool
}

type Snapper struct {
	args args

	mtx   sync.Mutex
	state State

	// set in state Plan, used in Waiting
	lastInvocation time.Time

	// valid for state Snapshotting
	plan map[*zfs.DatasetPath]*snapProgress

	// valid for state SyncUp and Waiting
	sleepUntil time.Time

	// valid for state Err
	err error
}

//go:generate stringer -type=State
type State uint

const (
	SyncUp State = 1 << iota
	SyncUpErrWait
	Planning
	Snapshotting
	Waiting
	ErrorWait
	Stopped
)

func (s State) sf() state {
	m := map[State]state{
		SyncUp:        syncUp,
		SyncUpErrWait: wait,
		Planning:      plan,
		Snapshotting:  snapshot,
		Waiting:       wait,
		ErrorWait:     wait,
		Stopped:       nil,
	}
	return m[s]
}

type updater func(u func(*Snapper)) State
type state func(a args, u updater) state

type Logger = logger.Logger

func getLogger(ctx context.Context) Logger {
	return logging.GetLogger(ctx, logging.SubsysSnapshot)
}

func PeriodicFromConfig(g *config.Global, fsf zfs.DatasetFilter, in *config.SnapshottingPeriodic) (*Snapper, error) {
	if in.Prefix == "" {
		return nil, errors.New("prefix must not be empty")
	}
	if in.Interval <= 0 {
		return nil, errors.New("interval must be positive")
	}

	hookList, err := hooks.ListFromConfig(&in.Hooks)
	if err != nil {
		return nil, errors.Wrap(err, "hook config error")
	}

	args := args{
		prefix:   in.Prefix,
		interval: in.Interval,
		fsf:      fsf,
		hooks:    hookList,
		// ctx and log is set in Run()
	}

	return &Snapper{state: SyncUp, args: args}, nil
}

func (s *Snapper) Run(ctx context.Context, snapshotsTaken chan<- struct{}) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()
	getLogger(ctx).Debug("start")
	defer getLogger(ctx).Debug("stop")

	s.args.snapshotsTaken = snapshotsTaken
	s.args.ctx = ctx
	s.args.dryRun = false // for future expansion

	u := func(u func(*Snapper)) State {
		s.mtx.Lock()
		defer s.mtx.Unlock()
		if u != nil {
			u(s)
		}
		return s.state
	}

	var st state = syncUp

	for st != nil {
		pre := u(nil)
		st = st(s.args, u)
		post := u(nil)
		getLogger(ctx).
			WithField("transition", fmt.Sprintf("%s=>%s", pre, post)).
			Debug("state transition")

	}

}

func onErr(err error, u updater) state {
	return u(func(s *Snapper) {
		s.err = err
		preState := s.state
		switch s.state {
		case SyncUp:
			s.state = SyncUpErrWait
		case Planning:
			fallthrough
		case Snapshotting:
			s.state = ErrorWait
		}
		getLogger(s.args.ctx).WithError(err).WithField("pre_state", preState).WithField("post_state", s.state).Error("snapshotting error")
	}).sf()
}

func onMainCtxDone(ctx context.Context, u updater) state {
	return u(func(s *Snapper) {
		s.err = ctx.Err()
		s.state = Stopped
	}).sf()
}

func syncUp(a args, u updater) state {
	u(func(snapper *Snapper) {
		snapper.lastInvocation = time.Now()
	})
	fss, err := listFSes(a.ctx, a.fsf)
	if err != nil {
		return onErr(err, u)
	}
	syncPoint, err := findSyncPoint(a.ctx, fss, a.prefix, a.interval)
	if err != nil {
		return onErr(err, u)
	}
	u(func(s *Snapper) {
		s.sleepUntil = syncPoint
	})
	t := time.NewTimer(time.Until(syncPoint))
	defer t.Stop()
	select {
	case <-t.C:
		return u(func(s *Snapper) {
			s.state = Planning
		}).sf()
	case <-a.ctx.Done():
		return onMainCtxDone(a.ctx, u)
	}
}

func plan(a args, u updater) state {
	u(func(snapper *Snapper) {
		snapper.lastInvocation = time.Now()
	})
	fss, err := listFSes(a.ctx, a.fsf)
	if err != nil {
		return onErr(err, u)
	}

	plan := make(map[*zfs.DatasetPath]*snapProgress, len(fss))
	for _, fs := range fss {
		plan[fs] = &snapProgress{state: SnapPending}
	}
	return u(func(s *Snapper) {
		s.state = Snapshotting
		s.plan = plan
		s.err = nil
	}).sf()
}

func snapshot(a args, u updater) state {

	var plan map[*zfs.DatasetPath]*snapProgress
	u(func(snapper *Snapper) {
		plan = snapper.plan
	})

	hookMatchCount := make(map[hooks.Hook]int, len(*a.hooks))
	for _, h := range *a.hooks {
		hookMatchCount[h] = 0
	}

	anyFsHadErr := false
	// TODO channel programs -> allow a little jitter?
	for fs, progress := range plan {
		suffix := time.Now().In(time.UTC).Format("20060102_150405_000")
		snapname := fmt.Sprintf("%s%s", a.prefix, suffix)

		ctx := logging.WithInjectedField(a.ctx, "fs", fs.ToString())
		ctx = logging.WithInjectedField(ctx, "snap", snapname)

		hookEnvExtra := hooks.Env{
			hooks.EnvFS:       fs.ToString(),
			hooks.EnvSnapshot: snapname,
		}

		jobCallback := hooks.NewCallbackHookForFilesystem("snapshot", fs, func(ctx context.Context) (err error) {
			l := getLogger(ctx)
			l.Debug("create snapshot")
			err = zfs.ZFSSnapshot(ctx, fs, snapname, false) // TODO propagate context to ZFSSnapshot
			if err != nil {
				l.WithError(err).Error("cannot create snapshot")
			}
			return
		})

		fsHadErr := false
		var planReport hooks.PlanReport
		var plan *hooks.Plan
		{
			filteredHooks, err := a.hooks.CopyFilteredForFilesystem(fs)
			if err != nil {
				getLogger(ctx).WithError(err).Error("unexpected filter error")
				fsHadErr = true
				goto updateFSState
			}
			// account for running hooks
			for _, h := range filteredHooks {
				hookMatchCount[h] = hookMatchCount[h] + 1
			}

			var planErr error
			plan, planErr = hooks.NewPlan(&filteredHooks, hooks.PhaseSnapshot, jobCallback, hookEnvExtra)
			if planErr != nil {
				fsHadErr = true
				getLogger(ctx).WithError(planErr).Error("cannot create job hook plan")
				goto updateFSState
			}
		}
		u(func(snapper *Snapper) {
			progress.name = snapname
			progress.startAt = time.Now()
			progress.hookPlan = plan
			progress.state = SnapStarted
		})
		{
			getLogger(ctx).WithField("report", plan.Report().String()).Debug("begin run job plan")
			plan.Run(ctx, a.dryRun)
			planReport = plan.Report()
			fsHadErr = planReport.HadError() // not just fatal errors
			if fsHadErr {
				getLogger(ctx).WithField("report", planReport.String()).Error("end run job plan with error")
			} else {
				getLogger(ctx).WithField("report", planReport.String()).Info("end run job plan successful")
			}
		}

	updateFSState:
		anyFsHadErr = anyFsHadErr || fsHadErr
		u(func(snapper *Snapper) {
			progress.doneAt = time.Now()
			progress.state = SnapDone
			if fsHadErr {
				progress.state = SnapError
			}
			progress.runResults = planReport
		})
	}

	select {
	case a.snapshotsTaken <- struct{}{}:
	default:
		if a.snapshotsTaken != nil {
			getLogger(a.ctx).Warn("callback channel is full, discarding snapshot update event")
		}
	}

	for h, mc := range hookMatchCount {
		if mc == 0 {
			hookIdx := -1
			for idx, ah := range *a.hooks {
				if ah == h {
					hookIdx = idx
					break
				}
			}
			getLogger(a.ctx).WithField("hook", h.String()).WithField("hook_number", hookIdx+1).Warn("hook did not match any snapshotted filesystems")
		}
	}

	return u(func(snapper *Snapper) {
		if anyFsHadErr {
			snapper.state = ErrorWait
			snapper.err = errors.New("one or more snapshots could not be created, check logs for details")
		} else {
			snapper.state = Waiting
			snapper.err = nil
		}
	}).sf()
}

func wait(a args, u updater) state {
	var sleepUntil time.Time
	u(func(snapper *Snapper) {
		lastTick := snapper.lastInvocation
		snapper.sleepUntil = lastTick.Add(a.interval)
		sleepUntil = snapper.sleepUntil
		log := getLogger(a.ctx).WithField("sleep_until", sleepUntil).WithField("duration", a.interval)
		logFunc := log.Debug
		if snapper.state == ErrorWait || snapper.state == SyncUpErrWait {
			logFunc = log.Error
		}
		logFunc("enter wait-state after error")
	})

	t := time.NewTimer(time.Until(sleepUntil))
	defer t.Stop()

	select {
	case <-t.C:
		return u(func(snapper *Snapper) {
			snapper.state = Planning
		}).sf()
	case <-a.ctx.Done():
		return onMainCtxDone(a.ctx, u)
	}
}

func listFSes(ctx context.Context, mf zfs.DatasetFilter) (fss []*zfs.DatasetPath, err error) {
	return zfs.ZFSListMapping(ctx, mf)
}

var syncUpWarnNoSnapshotUntilSyncupMinDuration = envconst.Duration("ZREPL_SNAPPER_SYNCUP_WARN_MIN_DURATION", 1*time.Second)

// see docs/snapshotting.rst
func findSyncPoint(ctx context.Context, fss []*zfs.DatasetPath, prefix string, interval time.Duration) (syncPoint time.Time, err error) {

	const (
		prioHasVersions int = iota
		prioNoVersions
	)

	type snapTime struct {
		ds   *zfs.DatasetPath
		prio int // lower is higher
		time time.Time
	}

	if len(fss) == 0 {
		return time.Now(), nil
	}

	snaptimes := make([]snapTime, 0, len(fss))
	hardErrs := 0

	now := time.Now()

	getLogger(ctx).Debug("examine filesystem state to find sync point")
	for _, d := range fss {
		ctx := logging.WithInjectedField(ctx, "fs", d.ToString())
		syncPoint, err := findSyncPointFSNextOptimalSnapshotTime(ctx, now, interval, prefix, d)
		if err == findSyncPointFSNoFilesystemVersionsErr {
			snaptimes = append(snaptimes, snapTime{
				ds:   d,
				prio: prioNoVersions,
				time: now,
			})
		} else if err != nil {
			hardErrs++
			getLogger(ctx).WithError(err).Error("cannot determine optimal sync point for this filesystem")
		} else {
			getLogger(ctx).WithField("syncPoint", syncPoint).Debug("found optimal sync point for this filesystem")
			snaptimes = append(snaptimes, snapTime{
				ds:   d,
				prio: prioHasVersions,
				time: syncPoint,
			})
		}
	}

	if hardErrs == len(fss) {
		return time.Time{}, fmt.Errorf("hard errors in determining sync point for every matching filesystem")
	}

	if len(snaptimes) == 0 {
		panic("implementation error: loop must either inc hardErrs or add result to snaptimes")
	}

	// sort ascending by (prio,time)
	// => those filesystems with versions win over those without any
	sort.Slice(snaptimes, func(i, j int) bool {
		if snaptimes[i].prio == snaptimes[j].prio {
			return snaptimes[i].time.Before(snaptimes[j].time)
		}
		return snaptimes[i].prio < snaptimes[j].prio
	})

	winnerSyncPoint := snaptimes[0].time
	l := getLogger(ctx).WithField("syncPoint", winnerSyncPoint.String())
	l.Info("determined sync point")
	if winnerSyncPoint.Sub(now) > syncUpWarnNoSnapshotUntilSyncupMinDuration {
		for _, st := range snaptimes {
			if st.prio == prioNoVersions {
				l.WithField("fs", st.ds.ToString()).Warn("filesystem will not be snapshotted until sync point")
			}
		}
	}

	return snaptimes[0].time, nil

}

var findSyncPointFSNoFilesystemVersionsErr = fmt.Errorf("no filesystem versions")

func findSyncPointFSNextOptimalSnapshotTime(ctx context.Context, now time.Time, interval time.Duration, prefix string, d *zfs.DatasetPath) (time.Time, error) {

	fsvs, err := zfs.ZFSListFilesystemVersions(ctx, d, zfs.ListFilesystemVersionsOptions{
		Types:           zfs.Snapshots,
		ShortnamePrefix: prefix,
	})
	if err != nil {
		return time.Time{}, errors.Wrap(err, "list filesystem versions")
	}
	if len(fsvs) <= 0 {
		return time.Time{}, findSyncPointFSNoFilesystemVersionsErr
	}

	// Sort versions by creation
	sort.SliceStable(fsvs, func(i, j int) bool {
		return fsvs[i].CreateTXG < fsvs[j].CreateTXG
	})

	latest := fsvs[len(fsvs)-1]
	getLogger(ctx).WithField("creation", latest.Creation).Debug("found latest snapshot")

	since := now.Sub(latest.Creation)
	if since < 0 {
		return time.Time{}, fmt.Errorf("snapshot %q is from the future: creation=%q now=%q", latest.ToAbsPath(d), latest.Creation, now)
	}

	return latest.Creation.Add(interval), nil
}
