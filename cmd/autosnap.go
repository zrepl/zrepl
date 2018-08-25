package cmd

import (
	"context"
	"fmt"
	"github.com/zrepl/zrepl/zfs"
	"sort"
	"time"
)

type IntervalAutosnap struct {
	task             *Task
	DatasetFilter    zfs.DatasetFilter
	Prefix           string
	SnapshotInterval time.Duration
}

func (a *IntervalAutosnap) filterFilesystems() (fss []*zfs.DatasetPath, stop bool) {
	a.task.Enter("filter_filesystems")
	defer a.task.Finish()
	fss, err := zfs.ZFSListMapping(a.DatasetFilter)
	stop = err != nil
	if err != nil {
		a.task.Log().WithError(err).Error("cannot list datasets")
	}
	if len(fss) == 0 {
		a.task.Log().Warn("no filesystem matching filesystem filter")
	}
	return fss, stop
}

func (a *IntervalAutosnap) findSyncPoint(fss []*zfs.DatasetPath) (syncPoint time.Time, err error) {
	a.task.Enter("find_sync_point")
	defer a.task.Finish()
	type snapTime struct {
		ds   *zfs.DatasetPath
		time time.Time
	}

	if len(fss) == 0 {
		return time.Now(), nil
	}

	snaptimes := make([]snapTime, 0, len(fss))

	now := time.Now()

	a.task.Log().Debug("examine filesystem state")
	for _, d := range fss {

		l := a.task.Log().WithField("fs", d.ToString())

		fsvs, err := zfs.ZFSListFilesystemVersions(d, NewPrefixFilter(a.Prefix))
		if err != nil {
			l.WithError(err).Error("cannot list filesystem versions")
			continue
		}
		if len(fsvs) <= 0 {
			l.WithField("prefix", a.Prefix).Info("no filesystem versions with prefix")
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
		if since < a.SnapshotInterval {
			next = latest.Creation.Add(a.SnapshotInterval)
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

func (a *IntervalAutosnap) waitForSyncPoint(ctx context.Context, syncPoint time.Time) {
	a.task.Enter("wait_sync_point")
	defer a.task.Finish()

	const LOG_TIME_FMT string = time.ANSIC

	a.task.Log().WithField("sync_point", syncPoint.Format(LOG_TIME_FMT)).
		Info("wait for sync point")

	select {
	case <-ctx.Done():
		a.task.Log().WithError(ctx.Err()).Info("context done")
		return
	case <-time.After(syncPoint.Sub(time.Now())):
	}
}

func (a *IntervalAutosnap) syncUpRun(ctx context.Context, didSnaps chan struct{}) (stop bool) {
	a.task.Enter("sync_up")
	defer a.task.Finish()

	fss, stop := a.filterFilesystems()
	if stop {
		return true
	}

	syncPoint, err := a.findSyncPoint(fss)
	if err != nil {
		return true
	}

	a.waitForSyncPoint(ctx, syncPoint)

	a.task.Log().Debug("snapshot all filesystems to enable further snaps in lockstep")
	a.doSnapshots(didSnaps)
	return false
}

func (a *IntervalAutosnap) Run(ctx context.Context, didSnaps chan struct{}) {

	if a.syncUpRun(ctx, didSnaps) {
		a.task.Log().Error("stoppping autosnap after error in sync up")
		return
	}

	// task drops back to idle here

	a.task.Log().Debug("setting up ticker in SnapshotInterval")
	ticker := time.NewTicker(a.SnapshotInterval)
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			a.task.Log().WithError(ctx.Err()).Info("context done")
			return

		case <-ticker.C:
			a.doSnapshots(didSnaps)
		}
	}

}

func (a *IntervalAutosnap) doSnapshots(didSnaps chan struct{}) {

	a.task.Enter("do_snapshots")
	defer a.task.Finish()

	// don't cache the result from previous run in case the user added
	// a new dataset in the meantime
	ds, stop := a.filterFilesystems()
	if stop {
		return
	}

	// TODO channel programs -> allow a little jitter?
	for _, d := range ds {
		suffix := time.Now().In(time.UTC).Format("20060102_150405_000")
		snapname := fmt.Sprintf("%s%s", a.Prefix, suffix)

		l := a.task.Log().
			WithField("fs", d.ToString()).
			WithField("snapname", snapname)

		l.Info("create snapshot")
		err := zfs.ZFSSnapshot(d, snapname, false)
		if err != nil {
			a.task.Log().WithError(err).Error("cannot create snapshot")
		}

		l.Info("create corresponding bookmark")
		err = zfs.ZFSBookmark(d, snapname, snapname)
		if err != nil {
			a.task.Log().WithError(err).Error("cannot create bookmark")
		}

	}

	select {
	case didSnaps <- struct{}{}:
	default:
		a.task.Log().Error("warning: callback channel is full, discarding")
	}

}
