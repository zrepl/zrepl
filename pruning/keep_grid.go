package pruning

import (
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/pruning/retentiongrid"
)

// KeepGrid fits snapshots that match a given regex into a retentiongrid.Grid,
// uses the most recent snapshot among those that match the regex as 'now',
// and deletes all snapshots that do not fit the grid specification.
type KeepGrid struct {
	retentionGrid *retentiongrid.Grid
	re            *regexp.Regexp
}

func NewKeepGrid(in *config.PruneGrid) (p *KeepGrid, err error) {

	if in.Regex == "" {
		return nil, fmt.Errorf("Regex must not be empty")
	}
	re, err := regexp.Compile(in.Regex)
	if err != nil {
		return nil, errors.Wrap(err, "Regex is invalid")
	}

	return newKeepGrid(re, in.Grid)
}

func MustNewKeepGrid(regex, gridspec string) *KeepGrid {

	ris, err := config.ParseRetentionIntervalSpec(gridspec)
	if err != nil {
		panic(err)
	}

	re := regexp.MustCompile(regex)

	grid, err := newKeepGrid(re, ris)
	if err != nil {
		panic(err)
	}
	return grid
}

func newKeepGrid(re *regexp.Regexp, configIntervals []config.RetentionInterval) (*KeepGrid, error) {
	if re == nil {
		panic("re must not be nil")
	}

	if len(configIntervals) == 0 {
		return nil, errors.New("retention grid must specify at least one interval")
	}

	intervals := make([]retentiongrid.Interval, len(configIntervals))
	for i := range configIntervals {
		intervals[i] = &configIntervals[i]
	}

	// Assert intervals are of increasing length (not necessarily required, but indicates config mistake)
	lastDuration := time.Duration(0)
	for i := range intervals {

		if intervals[i].Length() < lastDuration {
			// If all intervals before were keep=all, this is ok
			allPrevKeepCountAll := true
			for j := i - 1; allPrevKeepCountAll && j >= 0; j-- {
				allPrevKeepCountAll = intervals[j].KeepCount() == config.RetentionGridKeepCountAll
			}
			if allPrevKeepCountAll {
				goto isMonotonicIncrease
			}
			return nil, errors.New("retention grid interval length must be monotonically increasing")
		}
	isMonotonicIncrease:
		lastDuration = intervals[i].Length()
	}

	return &KeepGrid{
		retentionGrid: retentiongrid.NewGrid(intervals),
		re:            re,
	}, nil
}

type retentionGridAdaptor struct {
	Snapshot
}

func (a retentionGridAdaptor) LessThan(b retentiongrid.Entry) bool {
	return a.Date().Before(b.Date())
}

// Prune filters snapshots with the retention grid.
func (p *KeepGrid) KeepRule(snaps []Snapshot) (destroyList []Snapshot) {

	snaps = filterSnapList(snaps, func(snapshot Snapshot) bool {
		return p.re.MatchString(snapshot.Name())
	})
	if len(snaps) == 0 {
		return nil
	}

	// Build adaptors for retention grid
	adaptors := make([]retentiongrid.Entry, 0)
	for i := range snaps {
		adaptors = append(adaptors, retentionGridAdaptor{snaps[i]})
	}

	// determine 'now' edge
	sort.SliceStable(adaptors, func(i, j int) bool {
		return adaptors[i].LessThan(adaptors[j])
	})
	now := adaptors[len(adaptors)-1].Date()

	// Evaluate retention grid
	_, removea := p.retentionGrid.FitEntries(now, adaptors)

	// Revert adaptors
	destroyList = make([]Snapshot, len(removea))
	for i := range removea {
		destroyList[i] = removea[i].(retentionGridAdaptor).Snapshot
	}
	return destroyList
}
