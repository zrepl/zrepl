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

	// Assert intervals are of increasing length (not necessarily required, but indicates config mistake)
	lastDuration := time.Duration(0)
	for i := range in.Grid {

		if in.Grid[i].Length() < lastDuration {
			// If all intervals before were keep=all, this is ok
			allPrevKeepCountAll := true
			for j := i - 1; allPrevKeepCountAll && j >= 0; j-- {
				allPrevKeepCountAll = in.Grid[j].KeepCount() == config.RetentionGridKeepCountAll
			}
			if allPrevKeepCountAll {
				goto isMonotonicIncrease
			}
			err = errors.New("retention grid interval length must be monotonically increasing")
			return
		}
	isMonotonicIncrease:
		lastDuration = in.Grid[i].Length()

	}

	retentionIntervals := make([]retentiongrid.Interval, len(in.Grid))
	for i := range in.Grid {
		retentionIntervals[i] = &in.Grid[i]
	}

	return &KeepGrid{
		retentiongrid.NewGrid(retentionIntervals),
		re,
	}, nil
}

type retentionGridAdaptor struct {
	Snapshot
}

func (a retentionGridAdaptor) Date() time.Time { return a.Snapshot.GetCreation() }

func (a retentionGridAdaptor) LessThan(b retentiongrid.Entry) bool {
	return a.Date().Before(b.Date())
}

// Prune filters snapshots with the retention grid.
func (p *KeepGrid) KeepRule(snaps []Snapshot) PruneSnapshotsResult {

	reCandidates := partitionSnapList(snaps, func(snapshot Snapshot) bool {
		return p.re.MatchString(snapshot.GetName())
	})
	if len(reCandidates.Remove) == 0 {
		return reCandidates
	}

	// Build adaptors for retention grid
	adaptors := make([]retentiongrid.Entry, 0)
	for i := range snaps {
		adaptors = append(adaptors, retentionGridAdaptor{reCandidates.Remove[i]})
	}

	// determine 'now' edge
	sort.SliceStable(adaptors, func(i, j int) bool {
		return adaptors[i].LessThan(adaptors[j])
	})
	now := adaptors[len(adaptors)-1].Date()

	// Evaluate retention grid
	keepa, removea := p.retentionGrid.FitEntries(now, adaptors)

	// Revert adaptors
	destroyList := make([]Snapshot, len(removea))
	for i := range removea {
		destroyList[i] = removea[i].(retentionGridAdaptor).Snapshot
	}
	for _, a := range keepa {
		reCandidates.Keep = append(reCandidates.Keep, a.(retentionGridAdaptor))
	}
	reCandidates.Remove = destroyList

	return reCandidates
}
