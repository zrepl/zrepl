package cmd

import (
	"context"
	"fmt"
	"github.com/zrepl/zrepl/util"
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
	timer     time.Timer
}

type snapTime struct {
	ds   *zfs.DatasetPath
	time time.Time
}

func (a *IntervalAutosnap) Run(ctx context.Context) {

	a.log = ctx.Value(contextKeyLog).(Logger)

	const LOG_TIME_FMT string = time.ANSIC

	ds, err := zfs.ZFSListMapping(a.DatasetFilter)
	if err != nil {
		a.log.Printf("error listing datasets: %s", err)
		return
	}
	if len(ds) == 0 {
		a.log.Printf("no datasets matching dataset filter")
		return
	}

	a.snaptimes = make([]snapTime, len(ds))

	now := time.Now()

	a.log.Printf("examining filesystem state")
	for i, d := range ds {

		l := util.NewPrefixLogger(a.log, d.ToString())

		fsvs, err := zfs.ZFSListFilesystemVersions(d, &PrefixSnapshotFilter{a.Prefix})
		if err != nil {
			l.Printf("error listing filesystem versions of %s")
			continue
		}
		if len(fsvs) <= 0 {
			l.Printf("no filesystem versions with prefix '%s'", a.Prefix)
			a.snaptimes[i] = snapTime{d, now}
			continue
		}

		// Sort versions by creation
		sort.SliceStable(fsvs, func(i, j int) bool {
			return fsvs[i].CreateTXG < fsvs[j].CreateTXG
		})

		latest := fsvs[len(fsvs)-1]
		l.Printf("latest snapshot at %s (%s old)", latest.Creation.Format(LOG_TIME_FMT), now.Sub(latest.Creation))

		since := now.Sub(latest.Creation)
		if since < 0 {
			l.Printf("error: snapshot is from future (created at %s)", latest.Creation.Format(LOG_TIME_FMT))
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
	a.log.Printf("sync point at %s (in %s)", syncPoint.time.Format(LOG_TIME_FMT), syncPoint.time.Sub(now))

	select {
	case <-ctx.Done():
		a.log.Printf("context: %s", ctx.Err())
		return

	case <-time.After(syncPoint.time.Sub(now)):
		a.log.Printf("snapshotting all filesystems to enable further snaps in lockstep")
		a.doSnapshots()
	}

	ticker := time.NewTicker(a.SnapshotInterval)

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			a.log.Printf("context: %s", ctx.Err())
			return

		case <-ticker.C:
			a.doSnapshots()
		}
	}

}

func (a *IntervalAutosnap) doSnapshots() {

	// fetch new dataset list in case user added new dataset
	ds, err := zfs.ZFSListMapping(a.DatasetFilter)
	if err != nil {
		a.log.Printf("error listing datasets: %s", err)
		return
	}

	// TODO channel programs -> allow a little jitter?
	for _, d := range ds {
		suffix := time.Now().In(time.UTC).Format("20060102_150405_000")
		snapname := fmt.Sprintf("%s%s", a.Prefix, suffix)

		a.log.Printf("snapshotting %s@%s", d.ToString(), snapname)
		err := zfs.ZFSSnapshot(d, snapname, false)
		if err != nil {
			a.log.Printf("error snapshotting %s: %s", d.ToString(), err)
		}
	}

}
