package util

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"strconv"
	"strings"
	"testing"
	"time"
)

func retentionGridFromString(gs string) (g *RetentionGrid) {
	intervals := strings.Split(gs, "|")
	g = &RetentionGrid{
		intervals: make([]RetentionInterval, len(intervals)),
	}
	for idx, i := range intervals {
		comps := strings.SplitN(i, ",", 2)
		var durationStr, numSnapsStr string
		durationStr = comps[0]
		if len(comps) == 1 {
			numSnapsStr = "1"
		} else {
			numSnapsStr = comps[1]
		}

		var err error
		var interval RetentionInterval

		if interval.KeepCount, err = strconv.Atoi(numSnapsStr); err != nil {
			panic(err)
		}
		if interval.Length, err = time.ParseDuration(durationStr); err != nil {
			panic(err)
		}

		g.intervals[idx] = interval
	}
	return
}

type dummySnap struct {
	Name       string
	ShouldKeep bool
	date       time.Time
}

func (ds dummySnap) Date() time.Time {
	return ds.date
}

func (ds dummySnap) LessThan(b RetentionGridEntry) bool {
	return ds.date.Before(b.(dummySnap).date) // don't have a txg here
}

func validateRetentionGridFitEntries(t *testing.T, now time.Time, input, keep, remove []RetentionGridEntry) {

	snapDescr := func(d dummySnap) string {
		return fmt.Sprintf("%s@%s", d.Name, d.date.Sub(now))
	}

	t.Logf("keep list:\n")
	for k := range keep {
		t.Logf("\t%s\n", snapDescr(keep[k].(dummySnap)))
	}
	t.Logf("remove list:\n")
	for k := range remove {
		t.Logf("\t%s\n", snapDescr(remove[k].(dummySnap)))
	}

	t.Logf("\n\n")

	for _, s := range input {
		d := s.(dummySnap)
		descr := snapDescr(d)
		t.Logf("testing %s\n", descr)
		if d.ShouldKeep {
			assert.Contains(t, keep, d, "expecting %s to be kept", descr)
		} else {
			assert.Contains(t, remove, d, "expecting %s to be removed", descr)
		}
	}

	t.Logf("resulting list:\n")
	for k := range keep {
		t.Logf("\t%s\n", snapDescr(keep[k].(dummySnap)))
	}
}

func TestRetentionGridFitEntriesEmptyInput(t *testing.T) {
	g := retentionGridFromString("10m|10m|10m|1h")
	keep, remove := g.FitEntries(time.Now(), []RetentionGridEntry{})
	assert.Empty(t, keep)
	assert.Empty(t, remove)
}

func TestRetentionGridFitEntriesIntervalBoundariesAndAlignment(t *testing.T) {

	// Intervals are (duration], i.e. 10min is in the first interval, not in the second

	g := retentionGridFromString("10m|10m|10m")

	t.Logf("%#v\n", g)

	now := time.Unix(0, 0)

	snaps := []RetentionGridEntry{
		dummySnap{"0", true, now.Add(1 * time.Minute)},    // before now
		dummySnap{"1", true, now},                         // before now
		dummySnap{"2", true, now.Add(-10 * time.Minute)},  // 1st interval
		dummySnap{"3", true, now.Add(-20 * time.Minute)},  // 2nd interval
		dummySnap{"4", true, now.Add(-30 * time.Minute)},  // 3rd interval
		dummySnap{"5", false, now.Add(-40 * time.Minute)}, // after last interval
	}

	keep, remove := g.FitEntries(now, snaps)
	validateRetentionGridFitEntries(t, now, snaps, keep, remove)

}

func TestRetentionGridFitEntries(t *testing.T) {

	g := retentionGridFromString("10m,-1|10m|10m,2|1h")

	t.Logf("%#v\n", g)

	now := time.Unix(0, 0)

	snaps := []RetentionGridEntry{
		dummySnap{"1", true, now.Add(3 * time.Minute)},   // pre-now must always be kept
		dummySnap{"b1", true, now.Add(-6 * time.Minute)}, // 1st interval allows unlimited entries
		dummySnap{"b3", true, now.Add(-8 * time.Minute)}, // 1st interval allows unlimited entries
		dummySnap{"b2", true, now.Add(-9 * time.Minute)}, // 1st interval allows unlimited entries
		dummySnap{"a", false, now.Add(-11 * time.Minute)},
		dummySnap{"c", true, now.Add(-19 * time.Minute)}, // 2nd interval allows 1 entry
		dummySnap{"foo", false, now.Add(-25 * time.Minute)},
		dummySnap{"bar", true, now.Add(-26 * time.Minute)}, // 3rd interval allows 2 entries
		dummySnap{"border", true, now.Add(-30 * time.Minute)},
		dummySnap{"d", true, now.Add(-1*time.Hour - 15*time.Minute)},
		dummySnap{"e", false, now.Add(-1*time.Hour - 31*time.Minute)}, // before earliest interval must always be deleted
		dummySnap{"f", false, now.Add(-2 * time.Hour)},
	}
	keep, remove := g.FitEntries(now, snaps)

	validateRetentionGridFitEntries(t, now, snaps, keep, remove)

}
