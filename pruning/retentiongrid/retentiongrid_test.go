package retentiongrid

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type testInterval struct {
	length    time.Duration
	keepCount int
}

func (i *testInterval) Length() time.Duration { return i.length }
func (i *testInterval) KeepCount() int        { return i.keepCount }

func gridFromString(gs string) (g *Grid) {
	sintervals := strings.Split(gs, "|")
	intervals := make([]Interval, len(sintervals))
	for idx, i := range sintervals {
		comps := strings.SplitN(i, ",", 2)
		var durationStr, numSnapsStr string
		durationStr = comps[0]
		if len(comps) == 1 {
			numSnapsStr = "1"
		} else {
			numSnapsStr = comps[1]
		}

		var err error
		var interval testInterval

		if interval.keepCount, err = strconv.Atoi(numSnapsStr); err != nil {
			panic(err)
		}
		if interval.length, err = time.ParseDuration(durationStr); err != nil {
			panic(err)
		}

		intervals[idx] = &interval
	}
	return NewGrid(intervals)
}

type testSnap struct {
	Name       string
	ShouldKeep bool
	date       time.Time
}

func (ds testSnap) Date() time.Time { return ds.date }

func validateRetentionGridFitEntries(t *testing.T, now time.Time, input, keep, remove []Entry) {

	snapDescr := func(d testSnap) string {
		return fmt.Sprintf("%s@%s", d.Name, d.date.Sub(now))
	}

	t.Logf("keep list:\n")
	for k := range keep {
		t.Logf("\t%s\n", snapDescr(keep[k].(testSnap)))
	}
	t.Logf("remove list:\n")
	for k := range remove {
		t.Logf("\t%s\n", snapDescr(remove[k].(testSnap)))
	}

	t.Logf("\n\n")

	for _, s := range input {
		d := s.(testSnap)
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
		t.Logf("\t%s\n", snapDescr(keep[k].(testSnap)))
	}
}

func TestEmptyInput(t *testing.T) {
	g := gridFromString("10m|10m|10m|1h")
	keep, remove := g.FitEntries([]Entry{})
	assert.Empty(t, keep)
	assert.Empty(t, remove)
}

func TestIntervalBoundariesAndAlignment(t *testing.T) {
	g := gridFromString("10m|10m|10m")

	t.Logf("%#v\n", g)

	now := time.Unix(0, 0)

	snaps := []Entry{
		testSnap{"0", true, now.Add(1 * time.Minute)},    // before now => keep unconditionally
		testSnap{"1", true, now},                         // 1st interval left edge => inclusive
		testSnap{"2", true, now.Add(-10 * time.Minute)},  // 2nd interval left edge => inclusive
		testSnap{"3", true, now.Add(-20 * time.Minute)},  // 3rd interval left edge => inclusuive
		testSnap{"4", false, now.Add(-30 * time.Minute)}, // 3rd interval right edge => excludive
		testSnap{"5", false, now.Add(-40 * time.Minute)}, // after last interval => remove unconditionally
	}

	keep, remove := g.fitEntriesWithNow(now, snaps)
	validateRetentionGridFitEntries(t, now, snaps, keep, remove)
}

func TestKeepsOldestSnapsInABucket(t *testing.T) {
	g := gridFromString("1m,2")

	relt := func(secs int64) time.Time { return time.Unix(secs, 0) }

	snaps := []Entry{
		testSnap{"1", true, relt(1)},
		testSnap{"2", true, relt(2)},
		testSnap{"3", false, relt(3)},
		testSnap{"4", false, relt(4)},
		testSnap{"5", false, relt(5)},
	}

	now := relt(6)
	keep, remove := g.FitEntries(snaps)
	validateRetentionGridFitEntries(t, now, snaps, keep, remove)
}

func TestRespectsKeepCountAll(t *testing.T) {
	g := gridFromString("1m,-1|1m,1")
	relt := func(secs int64) time.Time { return time.Unix(secs, 0) }
	snaps := []Entry{
		testSnap{"a", true, relt(0)},
		testSnap{"b", true, relt(-1)},
		testSnap{"c", true, relt(-2)},
		testSnap{"d", false, relt(-60)},
		testSnap{"e", true, relt(-61)},
	}
	keep, remove := g.FitEntries(snaps)
	validateRetentionGridFitEntries(t, relt(61), snaps, keep, remove)
}

func TestComplex(t *testing.T) {

	g := gridFromString("10m,-1|10m|10m,2|1h")

	t.Logf("%#v\n", g)

	now := time.Unix(0, 0)

	snaps := []Entry{
		// pre-now must always be kept
		testSnap{"1", true, now.Add(3 * time.Minute)},
		// 1st interval allows unlimited entries
		testSnap{"b1", true, now.Add(-6 * time.Minute)},
		testSnap{"b3", true, now.Add(-8 * time.Minute)},
		testSnap{"b2", true, now.Add(-9 * time.Minute)},
		// 2nd interval allows 1 entry
		testSnap{"a", false, now.Add(-11 * time.Minute)},
		testSnap{"c", true, now.Add(-19 * time.Minute)},
		// 3rd interval allows 2 entries
		testSnap{"foo", true, now.Add(-25 * time.Minute)},
		testSnap{"bar", true, now.Add(-26 * time.Minute)},
		// this is at the left edge of the 4th interval
		testSnap{"border", false, now.Add(-30 * time.Minute)},
		// right in the 4th interval
		testSnap{"d", true, now.Add(-1*time.Hour - 15*time.Minute)},
		// on the right edge of 4th interval => not in it => delete
		testSnap{"q", false, now.Add(-1*time.Hour - 30*time.Minute)},
		// older then 4th interval => always delete
		testSnap{"e", false, now.Add(-1*time.Hour - 31*time.Minute)},
		testSnap{"f", false, now.Add(-2 * time.Hour)},
	}
	keep, remove := g.fitEntriesWithNow(now, snaps)

	validateRetentionGridFitEntries(t, now, snaps, keep, remove)

}
