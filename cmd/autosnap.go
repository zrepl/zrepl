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

	log       Logger
	snaptimes []snapTime
}

type snapTime struct {
	ds   *zfs.DatasetPath
	time time.Time
}

func (a *IntervalAutosnap) Run(ctx context.Context, didSnaps chan struct{}) {

	a.log = ctx.Value(contextKeyLog).(Logger)

	const LOG_TIME_FMT string = time.ANSIC

	ds, err := zfs.ZFSListMapping(a.DatasetFilter)
	if err != nil {
		a.log.WithError(err).Error("cannot list datasets")
		return
	}
	if len(ds) == 0 {
		a.log.WithError(err).Error("no datasets matching dataset filter")
		return
	}

	a.snaptimes = make([]snapTime, len(ds))

	now := time.Now()

	a.log.Debug("examine filesystem state")
	for i, d := range ds {

		l := a.log.WithField(logFSField, d.ToString())

		fsvs, err := zfs.ZFSListFilesystemVersions(d, &PrefixSnapshotFilter{a.Prefix})
		if err != nil {
			l.WithError(err).Error("cannot list filesystem versions")
			continue
		}
		if len(fsvs) <= 0 {
			l.WithField("prefix", a.Prefix).Info("no filesystem versions with prefix")
			a.snaptimes[i] = snapTime{d, now}
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
		a.snaptimes[i] = snapTime{d, next}
	}

	sort.Slice(a.snaptimes, func(i, j int) bool {
		return a.snaptimes[i].time.Before(a.snaptimes[j].time)
	})

	syncPoint := a.snaptimes[0]
	a.log.WithField("sync_point", syncPoint.time.Format(LOG_TIME_FMT)).
		Info("wait for sync point")

	select {
	case <-ctx.Done():
		a.log.WithError(ctx.Err()).Info("context done")
		return

	case <-time.After(syncPoint.time.Sub(now)):
		a.log.Debug("snapshot all filesystems to enable further snaps in lockstep")
		a.doSnapshots(didSnaps)
	}

	ticker := time.NewTicker(a.SnapshotInterval)

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			a.log.WithError(ctx.Err()).Info("context done")
			return

		case <-ticker.C:
			a.doSnapshots(didSnaps)
		}
	}

}

func (a *IntervalAutosnap) doSnapshots(didSnaps chan struct{}) {

	// fetch new dataset list in case user added new dataset
	ds, err := zfs.ZFSListMapping(a.DatasetFilter)
	if err != nil {
		a.log.WithError(err).Error("cannot list datasets")
		return
	}

	// TODO channel programs -> allow a little jitter?
	for _, d := range ds {
		suffix := time.Now().In(time.UTC).Format("20060102_150405_000")
		snapname := fmt.Sprintf("%s%s", a.Prefix, suffix)

		a.log.WithField(logFSField, d.ToString()).
			WithField("snapname", snapname).
			Info("create snapshot")

		err := zfs.ZFSSnapshot(d, snapname, false)
		if err != nil {
			a.log.WithError(err).Error("cannot create snapshot")
		}
	}

	select {
	case didSnaps <- struct{}{}:
	default:
		a.log.Warn("warning: callback channel is full, discarding")
	}

}
