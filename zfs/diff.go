package zfs

import (
	"errors"
	"fmt"
	"strings"
)

type VersionType string

const (
	Bookmark VersionType = "bookmark"
	Snapshot             = "snapshot"
)

type FilesystemVersion struct {
	Type VersionType
	Name string
	//ZFS_PROP_CREATETX and ZFS_PROP_GUID would be nice here => ZFS_PROP_CREATETX, libzfs_dataset.c:zfs_prop_get
}

/* The sender (left) wants to know if the receiver (right) has more recent versions

	Left :         | C |
	Right: | A | B | C | D | E |
	=>   :         | C | D | E |

	Left:         | C |
	Right:			  | D | E |
	=>   :  <empty list>, no common ancestor

	Left :         | C | D | E |
	Right: | A | B | C |
	=>   :  <empty list>, the left has newer versions

	Left : | A | B | C |       | F |
	Right:         | C | D | E |
	=>   :         | C |	   | F | => diverged => <empty list>

IMPORTANT: since ZFS currently does not export dataset UUIDs, the best heuristic to
		   identify a filesystem version is the tuple (name,creation)
*/
type FilesystemDiff struct {

	// The increments required to get left up to right's most recent version
	// 0th element is the common ancestor, ordered by birthtime, oldest first
	// If empty, left and right are at same most recent version
	// If nil, there is no incremental path for left to get to right's most recent version
	// This means either (check Diverged field to determine which case we are in)
	//   a) no common ancestor (left deleted all the snapshots it previously transferred to right)
	//		=> consult MRCAPathRight and request initial retransfer after prep on left side
	//   b) divergence bewteen left and right (left made snapshots that right doesn't have)
	//   	=> check MRCAPathLeft and MRCAPathRight and decide what to do based on that
	IncrementalPath []FilesystemVersion

	// true if left and right diverged, false otherwise
	Diverged bool
	// If Diverged, contains path from left most recent common ancestor (mrca)
	// to most recent version on left
	// Otherwise: nil
	MRCAPathLeft []FilesystemVersion
	// If  Diverged, contains path from right most recent common ancestor (mrca)
	// to most recent version on right
	// If there is no common ancestor (i.e. not diverged), contains entire list of
	// versions on right
	MRCAPathRight []FilesystemVersion
}

func ZFSListFilesystemVersions(fs DatasetPath) (res []FilesystemVersion, err error) {
	var fieldLines [][]string
	fieldLines, err = ZFSList(
		[]string{"name"},
		"-r", "-d", "1",
		"-t", "bookmark,snapshot",
		"-s", "creation", fs.ToString())
	if err != nil {
		return
	}
	res = make([]FilesystemVersion, len(fieldLines))
	for i, line := range fieldLines {

		if len(line[0]) < 3 {
			err = errors.New(fmt.Sprintf("snapshot or bookmark name implausibly short: %s", line[0]))
			return
		}

		snapSplit := strings.SplitN(line[0], "@", 2)
		bookmarkSplit := strings.SplitN(line[0], "#", 2)
		if len(snapSplit)*len(bookmarkSplit) != 2 {
			err = errors.New(fmt.Sprintf("dataset cannot be snapshot and bookmark at the same time: %s", line[0]))
			return
		}

		var v FilesystemVersion
		if len(snapSplit) == 2 {
			v.Name = snapSplit[1]
			v.Type = Snapshot
		} else {
			v.Name = bookmarkSplit[1]
			v.Type = Bookmark
		}

		res[i] = v

	}
	return
}

// we must assume left and right are ordered ascendingly by ZFS_PROP_CREATETXG and that
// names are unique (bas ZFS_PROP_GUID replacement)
func MakeFilesystemDiff(left, right []FilesystemVersion) (diff FilesystemDiff) {

	// Find most recent common ancestor by name, preferring snapshots over bookmars
	mrcaLeft := len(left) - 1
	var mrcaRight int
outer:
	for ; mrcaLeft >= 0; mrcaLeft-- {
		for i := len(right) - 1; i >= 0; i-- {
			if left[mrcaLeft].Name == right[i].Name {
				mrcaRight = i
				if i-1 >= 0 && right[i-1].Name == right[i].Name && right[i-1].Type == Snapshot {
					// prefer snapshots over bookmarks
					mrcaRight = i - 1
				}
				break outer
			}
		}
	}

	// no common ancestor?
	if mrcaLeft == -1 {
		diff = FilesystemDiff{
			IncrementalPath: nil,
			Diverged:        false,
			MRCAPathRight:   right,
		}
		return
	}

	// diverged?
	if mrcaLeft != len(left)-1 {
		diff = FilesystemDiff{
			IncrementalPath: nil,
			Diverged:        true,
			MRCAPathLeft:    left[mrcaLeft:],
			MRCAPathRight:   right[mrcaRight:],
		}
		return
	}

	if mrcaLeft != len(left)-1 {
		panic("invariant violated: mrca on left must be the last item in the left list")
	}

	// strip bookmarks going forward from right
	incPath := make([]FilesystemVersion, 0, len(right))
	incPath = append(incPath, right[mrcaRight])
	// right[mrcaRight] may be a bookmark if there's no equally named snapshot
	for i := mrcaRight + 1; i < len(right); i++ {
		if right[i].Type != Bookmark {
			incPath = append(incPath, right[i])
		}
	}

	diff = FilesystemDiff{
		IncrementalPath: incPath,
	}
	return
}
