package cmd

import (
	"fmt"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GridPrunePolicy struct {
	RetentionGrid *util.RetentionGrid
	MaxBookmarks  int
}

const GridPrunePolicyMaxBookmarksKeepAll = -1

type retentionGridAdaptor struct {
	zfs.FilesystemVersion
}

func (a retentionGridAdaptor) Date() time.Time {
	return a.Creation
}

func (a retentionGridAdaptor) LessThan(b util.RetentionGridEntry) bool {
	return a.CreateTXG < b.(retentionGridAdaptor).CreateTXG
}

// Prune filters snapshots with the retention grid.
// Bookmarks are deleted such that KeepBookmarks are kept in the end.
// The oldest bookmarks are removed first.
func (p *GridPrunePolicy) Prune(_ *zfs.DatasetPath, versions []zfs.FilesystemVersion) (keep, remove []zfs.FilesystemVersion, err error) {
	skeep, sremove := p.pruneSnapshots(versions)
	keep, remove = p.pruneBookmarks(skeep)
	remove = append(remove, sremove...)
	return keep, remove, nil
}

func (p *GridPrunePolicy) pruneSnapshots(versions []zfs.FilesystemVersion) (keep, remove []zfs.FilesystemVersion) {

	// Build adaptors for retention grid
	keep = []zfs.FilesystemVersion{}
	adaptors := make([]util.RetentionGridEntry, 0)
	for fsv := range versions {
		if versions[fsv].Type != zfs.Snapshot {
			keep = append(keep, versions[fsv])
			continue
		}
		adaptors = append(adaptors, retentionGridAdaptor{versions[fsv]})
	}

	sort.SliceStable(adaptors, func(i, j int) bool {
		return adaptors[i].LessThan(adaptors[j])
	})
	now := adaptors[len(adaptors)-1].Date()

	// Evaluate retention grid
	keepa, removea := p.RetentionGrid.FitEntries(now, adaptors)

	// Revert adaptors
	for i := range keepa {
		keep = append(keep, keepa[i].(retentionGridAdaptor).FilesystemVersion)
	}
	remove = make([]zfs.FilesystemVersion, len(removea))
	for i := range removea {
		remove[i] = removea[i].(retentionGridAdaptor).FilesystemVersion
	}
	return

}

func (p *GridPrunePolicy) pruneBookmarks(versions []zfs.FilesystemVersion) (keep, remove []zfs.FilesystemVersion) {

	if p.MaxBookmarks == GridPrunePolicyMaxBookmarksKeepAll {
		return versions, []zfs.FilesystemVersion{}
	}

	keep = []zfs.FilesystemVersion{}
	bookmarks := make([]zfs.FilesystemVersion, 0)
	for fsv := range versions {
		if versions[fsv].Type != zfs.Bookmark {
			keep = append(keep, versions[fsv])
			continue
		}
		bookmarks = append(bookmarks, versions[fsv])
	}

	if len(bookmarks) == 0 {
		return keep, []zfs.FilesystemVersion{}
	}
	if len(bookmarks) < p.MaxBookmarks {
		keep = append(keep, bookmarks...)
		return keep, []zfs.FilesystemVersion{}
	}

	// NOTE: sorting descending by descending by createtxg <=> sorting ascending wrt creation time
	sort.SliceStable(bookmarks, func(i, j int) bool {
		return (bookmarks[i].CreateTXG > bookmarks[j].CreateTXG)
	})

	keep = append(keep, bookmarks[:p.MaxBookmarks]...)
	remove = bookmarks[p.MaxBookmarks:]

	return keep, remove
}

func parseGridPrunePolicy(e map[string]interface{}, willSeeBookmarks bool) (p *GridPrunePolicy, err error) {

	const KeepBookmarksAllString = "all"
	var i struct {
		Grid          string
		KeepBookmarks string `mapstructure:"keep_bookmarks"`
	}

	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{Result: &i, WeaklyTypedInput: true})
	if err != nil {
		err = errors.Wrap(err, "mapstructure error")
		return
	}
	if err = dec.Decode(e); err != nil {
		err = errors.Wrapf(err, "mapstructure error")
		return
	}

	// Parse grid
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

	// Parse KeepBookmarks
	keepBookmarks := 0
	if i.KeepBookmarks == KeepBookmarksAllString || (i.KeepBookmarks == "" && !willSeeBookmarks) {
		keepBookmarks = GridPrunePolicyMaxBookmarksKeepAll
	} else {
		i, err := strconv.ParseInt(i.KeepBookmarks, 10, 32)
		if err != nil || i <= 0 || i > math.MaxInt32 {
			return nil, errors.Errorf("keep_bookmarks must be positive integer or 'all'")
		}
		keepBookmarks = int(i)
	}
	return &GridPrunePolicy{
		util.NewRetentionGrid(intervals),
		keepBookmarks,
	}, nil
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
