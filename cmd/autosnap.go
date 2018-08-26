package cmd

import (
	"context"
	"fmt"
	"github.com/zrepl/zrepl/zfs"
	"sort"
	"time"
)

type IntervalAutosnap struct {
	DatasetFilter    zfs.DatasetFilter
	Prefix           string
	SnapshotInterval time.Duration
}

func (a *IntervalAutosnap) filterFilesystems(ctx context.Context) (fss []*zfs.DatasetPath, stop bool) {
	fss, err := zfs.ZFSListMapping(a.DatasetFilter)
	stop = err != nil
	if err != nil {
		getLogger(ctx).WithError(err).Error("cannot list datasets")
	}
	if len(fss) == 0 {
		getLogger(ctx).Warn("no filesystem matching filesystem filter")
	}
	return fss, stop
}

func (a *IntervalAutosnap) findSyncPoint(log Logger, fss []*zfs.DatasetPath) (syncPoint time.Time, err error) {
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

	const LOG_TIME_FMT string = time.ANSIC

	getLogger(ctx).
		WithField("sync_point", syncPoint.Format(LOG_TIME_FMT)).
		Info("wait for sync point")

	select {
	case <-ctx.Done():
		getLogger(ctx).WithError(ctx.Err()).Info("context done")
		return
	case <-time.After(syncPoint.Sub(time.Now())):
	}
}

func (a *IntervalAutosnap) syncUpRun(ctx context.Context, didSnaps chan struct{}) (stop bool) {
	fss, stop := a.filterFilesystems(ctx)
	if stop {
		return true
	}

	syncPoint, err := a.findSyncPoint(getLogger(ctx), fss)
	if err != nil {
		return true
	}

	a.waitForSyncPoint(ctx, syncPoint)

	getLogger(ctx).Debug("snapshot all filesystems to enable further snaps in lockstep")
	a.doSnapshots(ctx, didSnaps)
	return false
}

func (a *IntervalAutosnap) Run(ctx context.Context, didSnaps chan struct{}) {

	log := getLogger(ctx)

	if a.syncUpRun(ctx, didSnaps) {
		log.Error("stoppping autosnap after error in sync up")
		return
	}

	// task drops back to idle here

	log.Debug("setting up ticker in SnapshotInterval")
	ticker := time.NewTicker(a.SnapshotInterval)
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			log.WithError(ctx.Err()).Info("context done")
			return
		case <-ticker.C:
			a.doSnapshots(ctx, didSnaps)
		}
	}

}

func (a *IntervalAutosnap) doSnapshots(ctx context.Context, didSnaps chan struct{}) {
	log := getLogger(ctx)

	// don't cache the result from previous run in case the user added
	// a new dataset in the meantime
	ds, stop := a.filterFilesystems(ctx)
	if stop {
		return
	}

	// TODO channel programs -> allow a little jitter?
	for _, d := range ds {
		suffix := time.Now().In(time.UTC).Format("20060102_150405_000")
		snapname := fmt.Sprintf("%s%s", a.Prefix, suffix)

		l := log.
			WithField("fs", d.ToString()).
			WithField("snapname", snapname)

		l.Info("create snapshot")
		err := zfs.ZFSSnapshot(d, snapname, false)
		if err != nil {
			l.WithError(err).Error("cannot create snapshot")
		}

		l.Info("create corresponding bookmark")
		err = zfs.ZFSBookmark(d, snapname, snapname)
		if err != nil {
			l.WithError(err).Error("cannot create bookmark")
		}

	}

	select {
	case didSnaps <- struct{}{}:
	default:
		log.Error("warning: callback channel is full, discarding")
	}

}
