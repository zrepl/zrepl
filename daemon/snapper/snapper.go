package snapper

import (
	"github.com/zrepl/zrepl/config"
	"github.com/pkg/errors"
	"time"
	"context"
	"github.com/zrepl/zrepl/daemon/filters"
	"fmt"
	"github.com/zrepl/zrepl/zfs"
	"sort"
	"github.com/zrepl/zrepl/logger"
	"sync"
)


//go:generate stringer -type=SnapState
type SnapState uint

const (
	SnapPending SnapState = 1 << iota
	SnapStarted
	SnapDone
	SnapError
)

type snapProgress struct {
	state SnapState

	// SnapStarted, SnapDone, SnapError
	name string
	startAt time.Time

	// SnapDone
	doneAt time.Time

	// SnapErr
	err error
}

type args struct {
	ctx            context.Context
	log            Logger
	prefix         string
	interval       time.Duration
	fsf            *filters.DatasetMapFilter
	snapshotsTaken chan<-struct{}
}

type Snapper struct {
	args args

	mtx sync.Mutex
	state State

	// set in state Plan, used in Waiting
	lastInvocation time.Time

	// valid for state Snapshotting
	plan map[*zfs.DatasetPath]snapProgress

	// valid for state SyncUp and Waiting
	sleepUntil time.Time

	// valid for state Err
	err error
}

//go:generate stringer -type=State
type State uint

const (
	SyncUp State = 1<<iota
	Planning
	Snapshotting
	Waiting
	ErrorWait
)

func (s State) sf() state {
	m := map[State]state{
		SyncUp:       syncUp,
		Planning:     plan,
		Snapshotting: snapshot,
		Waiting:      wait,
		ErrorWait:    wait,
	}
	return m[s]
}

type updater func(u func(*Snapper)) State
type state func(a args, u updater) state

type contextKey int

const (
	contextKeyLog contextKey = 0
)

type Logger = logger.Logger

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLog, log)
}

func getLogger(ctx context.Context) Logger {
	if log, ok := ctx.Value(contextKeyLog).(Logger); ok {
		return log
	}
	return logger.NewNullLogger()
}

func FromConfig(g *config.Global, fsf *filters.DatasetMapFilter, in *config.Snapshotting) (*Snapper, error) {
	if in.SnapshotPrefix == "" {
		return nil, errors.New("prefix must not be empty")
	}
	if in.Interval <= 0 {
		return nil, errors.New("interval must be positive")
	}

	args := args{
		prefix: in.SnapshotPrefix,
		interval: in.Interval,
		fsf: fsf,
		// ctx and log is set in Run()
	}

	return &Snapper{state: SyncUp, args: args}, nil
}

func (s *Snapper) Run(ctx context.Context, snapshotsTaken chan<- struct{}) {

	getLogger(ctx).Debug("start")
	defer getLogger(ctx).Debug("stop")

	s.args.snapshotsTaken = snapshotsTaken
	s.args.ctx = ctx
	s.args.log = getLogger(ctx)

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
		s.state = ErrorWait
	}).sf()
}

func syncUp(a args, u updater) state {
	fss, err := listFSes(a.fsf)
	if err != nil {
		return onErr(err, u)
	}
	syncPoint, err := findSyncPoint(a.log, fss, a.prefix, a.interval)
	if err != nil {
		return onErr(err, u)
	}
	u(func(s *Snapper){
		s.sleepUntil = syncPoint
	})
	t := time.NewTimer(syncPoint.Sub(time.Now()))
	defer t.Stop()
	select {
	case <-t.C:
		return u(func(s *Snapper) {
			s.state = Planning
		}).sf()
	case <-a.ctx.Done():
		return onErr(err, u)
	}
}

func plan(a args, u updater) state {
	u(func(snapper *Snapper) {
		snapper.lastInvocation = time.Now()
	})
	fss, err := listFSes(a.fsf)
	if err != nil {
		return onErr(err, u)
	}

	plan := make(map[*zfs.DatasetPath]snapProgress, len(fss))
	for _, fs := range fss {
		plan[fs] = snapProgress{state: SnapPending}
	}
	return u(func(s *Snapper) {
		s.state = Snapshotting
		s.plan = plan
	}).sf()
}

func snapshot(a args, u updater) state {

	var plan map[*zfs.DatasetPath]snapProgress
	u(func(snapper *Snapper) {
		plan = snapper.plan
	})

	hadErr := false
	// TODO channel programs -> allow a little jitter?
	for fs, progress := range plan {
		suffix := time.Now().In(time.UTC).Format("20060102_150405_000")
		snapname := fmt.Sprintf("%s%s", a.prefix, suffix)

		l := a.log.
			WithField("fs", fs.ToString()).
			WithField("snap", snapname)

		u(func(snapper *Snapper) {
			progress.name = snapname
			progress.startAt = time.Now()
			progress.state = SnapStarted
		})

		l.Debug("create snapshot")
		err := zfs.ZFSSnapshot(fs, snapname, false)
		if err != nil {
			hadErr = true
			l.WithError(err).Error("cannot create snapshot")
		}
		doneAt := time.Now()

		u(func(snapper *Snapper) {
			progress.doneAt = doneAt
			progress.state = SnapDone
			if err != nil {
				progress.state = SnapError
				progress.err = err
			}
		})
	}

	select {
	case a.snapshotsTaken <- struct{}{}:
	default:
		if a.snapshotsTaken != nil {
			a.log.Warn("callback channel is full, discarding snapshot update event")
		}
	}

	return u(func(snapper *Snapper) {
		if hadErr {
			snapper.state = ErrorWait
			snapper.err = errors.New("one or more snapshots could not be created, check logs for details")
		} else {
			snapper.state = Waiting
		}
	}).sf()
}

func wait(a args, u updater) state {
	var sleepUntil time.Time
	u(func(snapper *Snapper) {
		lastTick := snapper.lastInvocation
		snapper.sleepUntil = lastTick.Add(a.interval)
		sleepUntil = snapper.sleepUntil
	})

	t := time.NewTimer(sleepUntil.Sub(time.Now()))
	defer t.Stop()

	select {
	case <-t.C:
		return u(func(snapper *Snapper) {
			snapper.state = Planning
		}).sf()
	case <-a.ctx.Done():
		return onErr(a.ctx.Err(), u)
	}
}

func listFSes(mf *filters.DatasetMapFilter) (fss []*zfs.DatasetPath, err error) {
	return zfs.ZFSListMapping(mf)
}

func findSyncPoint(log Logger, fss []*zfs.DatasetPath, prefix string, interval time.Duration) (syncPoint time.Time, err error) {
	type snapTime struct {
		ds   *zfs.DatasetPath
		time time.Time
	}

	if len(fss) == 0 {
		return time.Now(), nil
	}

	snaptimes := make([]snapTime, 0, len(fss))

	now := time.Now()

	log.Debug("examine filesystem state")
	for _, d := range fss {

		l := log.WithField("fs", d.ToString())

		fsvs, err := zfs.ZFSListFilesystemVersions(d, filters.NewTypedPrefixFilter(prefix, zfs.Snapshot))
		if err != nil {
			l.WithError(err).Error("cannot list filesystem versions")
			continue
		}
		if len(fsvs) <= 0 {
			l.WithField("prefix", prefix).Debug("no filesystem versions with prefix")
			continue
		}

		// Sort versions by creation
		sort.SliceStable(fsvs, func(i, j int) bool {
			return fsvs[i].CreateTXG < fsvs[j].CreateTXG
		})

		latest := fsvs[len(fsvs)-1]
		l.WithField("creation", latest.Creation).
			Debug("found latest snapshot")

		since := now.Sub(latest.Creation)
		if since < 0 {
			l.WithField("snapshot", latest.Name).
				WithField("creation", latest.Creation).
				Error("snapshot is from the future")
			continue
		}
		next := now
		if since < interval {
			next = latest.Creation.Add(interval)
		}
		snaptimes = append(snaptimes, snapTime{d, next})
	}

	if len(snaptimes) == 0 {
		snaptimes = append(snaptimes, snapTime{nil, now})
	}

	sort.Slice(snaptimes, func(i, j int) bool {
		return snaptimes[i].time.Before(snaptimes[j].time)
	})

	return snaptimes[0].time, nil

}

