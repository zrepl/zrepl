package retentiongrid

import (
	"sort"
	"time"
)

type Interval interface {
	Length() time.Duration
	KeepCount() int
}

const RetentionGridKeepCountAll int = -1

type Grid struct {
	intervals []Interval
}

type Entry interface {
	Date() time.Time
}

func NewGrid(l []Interval) *Grid {
	if len(l) == 0 {
		panic("must specify at least one interval")
	}
	// TODO Maybe check for ascending interval lengths here, although the algorithm
	// 		itself doesn't care about that.
	return &Grid{l}
}

func (g Grid) FitEntries(entries []Entry) (keep, remove []Entry) {

	if len(entries) == 0 {
		return
	}

	// determine 'now' based on youngest snapshot
	// => sort youngest-to-oldest
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Date().After(entries[j].Date())
	})
	now := entries[0].Date()

	return g.fitEntriesWithNow(now, entries)
}

type bucket struct {
	keepCount     int
	youngerThan   time.Time
	olderThanOrEq time.Time
	entries       []Entry
}

func makeBucketFromInterval(olderThanOrEq time.Time, i Interval) bucket {
	var b bucket
	kc := i.KeepCount()
	if kc == 0 {
		panic("keep count 0 is not allowed")
	}
	if (kc < 0) && kc != RetentionGridKeepCountAll {
		panic("negative keep counts are not allowed")
	}
	b.keepCount = kc
	b.olderThanOrEq = olderThanOrEq
	b.youngerThan = b.olderThanOrEq.Add(-i.Length())
	return b
}

func (b *bucket) Contains(e Entry) bool {
	d := e.Date()
	olderThan := d.Before(b.olderThanOrEq)
	eq := d.Equal(b.olderThanOrEq)
	youngerThan := d.After(b.youngerThan)
	return (olderThan || eq) && youngerThan
}

func (b *bucket) AddIfContains(e Entry) (added bool) {
	added = b.Contains(e)
	if added {
		b.entries = append(b.entries, e)
	}
	return
}

func (b *bucket) RemoveYoungerSnapsExceedingKeepCount() (removed []Entry) {

	if b.keepCount == RetentionGridKeepCountAll {
		return nil
	}

	removeCount := len(b.entries) - b.keepCount
	if removeCount <= 0 {
		return nil
	}

	// sort youngest-to-oldest
	sort.SliceStable(b.entries, func(i, j int) bool {
		return b.entries[i].Date().After(b.entries[j].Date())
	})

	return b.entries[:removeCount]
}

func (g Grid) fitEntriesWithNow(now time.Time, entries []Entry) (keep, remove []Entry) {

	buckets := make([]bucket, len(g.intervals))

	buckets[0] = makeBucketFromInterval(now, g.intervals[0])
	for i := 1; i < len(g.intervals); i++ {
		buckets[i] = makeBucketFromInterval(buckets[i-1].youngerThan, g.intervals[i])
	}

	keep = make([]Entry, 0)
	remove = make([]Entry, 0)

assignEntriesToBuckets:
	for ei := 0; ei < len(entries); ei++ {
		e := entries[ei]
		// unconditionally keep entries that are in the future
		if now.Before(e.Date()) {
			keep = append(keep, e)
			continue assignEntriesToBuckets
		}
		// add to matching bucket, if any
		for bi := range buckets {
			if buckets[bi].AddIfContains(e) {
				continue assignEntriesToBuckets
			}
		}
		// unconditionally remove entries older than the oldest bucket
		remove = append(remove, e)
	}

	// now apply the `KeepCount` per bucket
	for _, b := range buckets {
		destroy := b.RemoveYoungerSnapsExceedingKeepCount()
		remove = append(remove, destroy...)
		keep = append(keep, b.entries...)
	}
	return
}
