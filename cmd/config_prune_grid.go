package cmd

import (
	"fmt"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GridPrunePolicy struct {
	RetentionGrid *util.RetentionGrid
}

type retentionGridAdaptor struct {
	zfs.FilesystemVersion
}

func (a retentionGridAdaptor) Date() time.Time {
	return a.Creation
}

func (a retentionGridAdaptor) LessThan(b util.RetentionGridEntry) bool {
	return a.CreateTXG < b.(retentionGridAdaptor).CreateTXG
}

func (p *GridPrunePolicy) Prune(_ *zfs.DatasetPath, versions []zfs.FilesystemVersion) (keep, remove []zfs.FilesystemVersion, err error) {

	// Build adaptors for retention grid
	adaptors := make([]util.RetentionGridEntry, len(versions))
	for fsv := range versions {
		adaptors[fsv] = retentionGridAdaptor{versions[fsv]}
	}

	sort.SliceStable(adaptors, func(i, j int) bool {
		return adaptors[i].LessThan(adaptors[j])
	})
	now := adaptors[len(adaptors)-1].Date()

	// Evaluate retention grid
	keepa, removea := p.RetentionGrid.FitEntries(now, adaptors)

	// Revert adaptors
	keep = make([]zfs.FilesystemVersion, len(keepa))
	for i := range keepa {
		keep[i] = keepa[i].(retentionGridAdaptor).FilesystemVersion
	}
	remove = make([]zfs.FilesystemVersion, len(removea))
	for i := range removea {
		remove[i] = removea[i].(retentionGridAdaptor).FilesystemVersion
	}
	return

}

func parseGridPrunePolicy(e map[string]interface{}) (p *GridPrunePolicy, err error) {

	var i struct {
		Grid string
	}

	if err = mapstructure.Decode(e, &i); err != nil {
		err = errors.Wrapf(err, "mapstructure error")
		return
	}

	p = &GridPrunePolicy{}

	// Parse grid policy
	intervals, err := parseRetentionGridIntervalsString(i.Grid)
	if err != nil {
		err = fmt.Errorf("cannot parse retention grid: %s", err)
		return
	}
	// Assert intervals are of increasing length (not necessarily required, but indicates config mistake)
	lastDuration := time.Duration(0)
	for i := range intervals {

		if intervals[i].Length < lastDuration {
			// If all intervals before were keep=all, this is ok
			allPrevKeepCountAll := true
			for j := i - 1; allPrevKeepCountAll && j >= 0; j-- {
				allPrevKeepCountAll = intervals[j].KeepCount == util.RetentionGridKeepCountAll
			}
			if allPrevKeepCountAll {
				goto isMonotonicIncrease
			}
			err = errors.New("retention grid interval length must be monotonically increasing")
			return
		}
	isMonotonicIncrease:
		lastDuration = intervals[i].Length

	}
	p.RetentionGrid = util.NewRetentionGrid(intervals)

	return
}

var retentionStringIntervalRegex *regexp.Regexp = regexp.MustCompile(`^\s*(\d+)\s*x\s*([^\(]+)\s*(\((.*)\))?\s*$`)

func parseRetentionGridIntervalString(e string) (intervals []util.RetentionInterval, err error) {

	comps := retentionStringIntervalRegex.FindStringSubmatch(e)
	if comps == nil {
		err = fmt.Errorf("retention string does not match expected format")
		return
	}

	times, err := strconv.Atoi(comps[1])
	if err != nil {
		return nil, err
	} else if times <= 0 {
		return nil, fmt.Errorf("contains factor <= 0")
	}

	duration, err := parsePostitiveDuration(comps[2])
	if err != nil {
		return nil, err
	}

	keepCount := 1
	if comps[3] != "" {
		// Decompose key=value, comma separated
		// For now, only keep_count is supported
		re := regexp.MustCompile(`^\s*keep=(.+)\s*$`)
		res := re.FindStringSubmatch(comps[4])
		if res == nil || len(res) != 2 {
			err = fmt.Errorf("interval parameter contains unknown parameters")
			return
		}
		if res[1] == "all" {
			keepCount = util.RetentionGridKeepCountAll
		} else {
			keepCount, err = strconv.Atoi(res[1])
			if err != nil {
				err = fmt.Errorf("cannot parse keep_count value")
				return
			}
		}
	}

	intervals = make([]util.RetentionInterval, times)
	for i := range intervals {
		intervals[i] = util.RetentionInterval{
			Length:    duration,
			KeepCount: keepCount,
		}
	}

	return

}

func parseRetentionGridIntervalsString(s string) (intervals []util.RetentionInterval, err error) {

	ges := strings.Split(s, "|")
	intervals = make([]util.RetentionInterval, 0, 7*len(ges))

	for intervalIdx, e := range ges {
		parsed, err := parseRetentionGridIntervalString(e)
		if err != nil {
			return nil, fmt.Errorf("cannot parse interval %d of %d: %s: %s", intervalIdx+1, len(ges), err, strings.TrimSpace(e))
		}
		intervals = append(intervals, parsed...)
	}

	return
}
