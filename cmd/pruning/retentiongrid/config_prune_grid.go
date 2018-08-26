package retentiongrid

import (
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/cmd/config"
	"github.com/zrepl/zrepl/zfs"
	"math"
	"sort"
	"strconv"
	"time"
)

type GridPrunePolicy struct {
	retentionGrid *retentionGrid
	keepBookmarks int
}

const GridPrunePolicyMaxBookmarksKeepAll = -1

type retentionGridAdaptor struct {
	zfs.FilesystemVersion
}

func (a retentionGridAdaptor) Date() time.Time {
	return a.Creation
}

func (a retentionGridAdaptor) LessThan(b RetentionGridEntry) bool {
	return a.CreateTXG < b.(retentionGridAdaptor).CreateTXG
}

// Prune filters snapshots with the retention grid.
// Bookmarks are deleted such that keepBookmarks are kept in the end.
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
	adaptors := make([]RetentionGridEntry, 0)
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
	keepa, removea := p.retentionGrid.FitEntries(now, adaptors)

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

	if p.keepBookmarks == GridPrunePolicyMaxBookmarksKeepAll {
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
	if len(bookmarks) < p.keepBookmarks {
		keep = append(keep, bookmarks...)
		return keep, []zfs.FilesystemVersion{}
	}

	// NOTE: sorting descending by descending by createtxg <=> sorting ascending wrt creation time
	sort.SliceStable(bookmarks, func(i, j int) bool {
		return (bookmarks[i].CreateTXG > bookmarks[j].CreateTXG)
	})

	keep = append(keep, bookmarks[:p.keepBookmarks]...)
	remove = bookmarks[p.keepBookmarks:]

	return keep, remove
}

func ParseGridPrunePolicy(in config.PruneGrid, willSeeBookmarks bool) (p *GridPrunePolicy, err error) {

	const KeepBookmarksAllString = "all"

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

	// Parse keepBookmarks
	keepBookmarks := 0
	if in.KeepBookmarks == KeepBookmarksAllString || (in.KeepBookmarks == "" && !willSeeBookmarks) {
		keepBookmarks = GridPrunePolicyMaxBookmarksKeepAll
	} else {
		i, err := strconv.ParseInt(in.KeepBookmarks, 10, 32)
		if err != nil || i <= 0 || i > math.MaxInt32 {
			return nil, errors.Errorf("keep_bookmarks must be positive integer or 'all'")
		}
		keepBookmarks = int(i)
	}

	retentionIntervals := make([]RetentionInterval, len(in.Grid))
	for i := range in.Grid {
		retentionIntervals[i] = &in.Grid[i]
	}

	return &GridPrunePolicy{
		newRetentionGrid(retentionIntervals),
		keepBookmarks,
	}, nil
}
