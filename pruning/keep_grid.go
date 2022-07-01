package pruning

import (
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/pruning/retentiongrid"
	"github.com/zrepl/zrepl/zfs"
)

// KeepGrid fits snapshots that match a given regex into a retentiongrid.Grid,
// uses the most recent snapshot among those that match the regex as 'now',
// and deletes all snapshots that do not fit the grid specification.
type KeepGrid struct {
	KeepCommon
	retentionGrid *retentiongrid.Grid
}

func NewKeepGrid(in *config.PruneGrid) (*KeepGrid, error) {

	if in.Regex == "" {
		return nil, fmt.Errorf("Regex must not be empty")
	}

	kc, err := newKeepCommon(&in.PruneKeepCommon)
	if err != nil {
		return nil, err
	}

	return newKeepGrid(kc, in.Grid)
}

func MustNewKeepGrid(filesystems config.FilesystemsFilter, regex, gridspec string) *KeepGrid {
	ris, err := config.ParseRetentionIntervalSpec(gridspec)
	if err != nil {
		panic(err)
	}

	if regex == "" {
		panic("Regex must not be empty")
	}

	kc, err := newKeepCommon(&config.PruneKeepCommon{
		Filesystems: filesystems,
		Regex:       regex,
	})
	if err != nil {
		panic(err)
	}

	grid, err := newKeepGrid(kc, ris)
	if err != nil {
		panic(err)
	}
	return grid
}

func newKeepGrid(common KeepCommon, configIntervals []config.RetentionInterval) (*KeepGrid, error) {

	if common.re == nil {
		panic("re must not be nil")
	}

	if common.fsf == nil {
		panic("fsf must not be nil")
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
		common,
		retentiongrid.NewGrid(intervals),
	}, nil
}

func (p *KeepGrid) GetFSFilter() zfs.DatasetFilter {
	return p.fsf
}

// Prune filters snapshots with the retention grid.
func (p *KeepGrid) KeepRule(snaps []Snapshot) (destroyList []Snapshot) {

	matching, notMatching := partitionSnapList(snaps, func(snapshot Snapshot) bool {
		return p.re.MatchString(snapshot.Name())
	})

	// snaps that don't match the regex are not kept by this rule
	destroyList = append(destroyList, notMatching...)

	if len(matching) == 0 {
		return destroyList
	}

	// Evaluate retention grid
	entrySlice := make([]retentiongrid.Entry, 0)
	for i := range matching {
		entrySlice = append(entrySlice, matching[i])
	}
	_, gridDestroyList := p.retentionGrid.FitEntries(entrySlice)

	// Revert adaptors
	for i := range gridDestroyList {
		destroyList = append(destroyList, gridDestroyList[i].(Snapshot))
	}
	return destroyList
}
