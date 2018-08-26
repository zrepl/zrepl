package retentiongrid

import (
	"sort"
	"time"
)

type RetentionInterval interface {
	Length() time.Duration
	KeepCount() int
}

const RetentionGridKeepCountAll int = -1

type retentionGrid struct {
	intervals []RetentionInterval
}

//A point inside the grid, i.e. a thing the grid can decide to remove
type RetentionGridEntry interface {
	Date() time.Time
	LessThan(b RetentionGridEntry) bool
}

func dateInInterval(date, startDateInterval time.Time, i RetentionInterval) bool {
	return date.After(startDateInterval) && date.Before(startDateInterval.Add(i.Length()))
}

func newRetentionGrid(l []RetentionInterval) *retentionGrid {
	// TODO Maybe check for ascending interval lengths here, although the algorithm
	// 		itself doesn't care about that.
	return &retentionGrid{l}
}

// Partition a list of RetentionGridEntries into the retentionGrid,
// relative to a given start date `now`.
//
// The `keepCount` oldest entries per `RetentionInterval` are kept (`keep`),
// the others are removed (`remove`).
//
// Entries that are younger than `now` are always kept.
// Those that are older than the earliest beginning of an interval are removed.
func (g retentionGrid) FitEntries(now time.Time, entries []RetentionGridEntry) (keep, remove []RetentionGridEntry) {

	type bucket struct {
		entries []RetentionGridEntry
	}
	buckets := make([]bucket, len(g.intervals))

	keep = make([]RetentionGridEntry, 0)
	remove = make([]RetentionGridEntry, 0)

	oldestIntervalStart := now
	for i := range g.intervals {
		oldestIntervalStart = oldestIntervalStart.Add(-g.intervals[i].Length())
	}

	for ei := 0; ei < len(entries); ei++ {
		e := entries[ei]

		date := e.Date()

		if date == now || date.After(now) {
			keep = append(keep, e)
			continue
		} else if date.Before(oldestIntervalStart) {
			remove = append(remove, e)
			continue
		}

		iStartTime := now
		for i := 0; i < len(g.intervals); i++ {
			iStartTime = iStartTime.Add(-g.intervals[i].Length())
			if date == iStartTime || dateInInterval(date, iStartTime, g.intervals[i]) {
				buckets[i].entries = append(buckets[i].entries, e)
			}
		}
	}

	for bi, b := range buckets {

		interval := g.intervals[bi]

		sort.SliceStable(b.entries, func(i, j int) bool {
			return b.entries[i].LessThan((b.entries[j]))
		})

		i := 0
		for ; (interval.KeepCount() == RetentionGridKeepCountAll || i < interval.KeepCount()) && i < len(b.entries); i++ {
			keep = append(keep, b.entries[i])
		}
		for ; i < len(b.entries); i++ {
			remove = append(remove, b.entries[i])
		}

	}

	return

}
