package zfs

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"os/exec"
	"sort"
)

type fsbyCreateTXG []FilesystemVersion

func (l fsbyCreateTXG) Len() int      { return len(l) }
func (l fsbyCreateTXG) Swap(i, j int) { l[i], l[j] = l[j], l[i] }
func (l fsbyCreateTXG) Less(i, j int) bool {
	return l[i].CreateTXG < l[j].CreateTXG
}

type Conflict int

const (
	ConflictIncremental      = 0 // no conflict, incremental repl possible
	ConflictAllRight         = 1 // no conflict, initial repl possible
	ConflictNoCommonAncestor = 2
	ConflictDiverged         = 3
)

/* The receiver (left) wants to know if the sender (right) has more recent versions

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

	// Which kind of conflict / "way forward" is possible.
	// Check this first to determine the semantics of this struct's remaining members
	Conflict Conflict

	// Conflict = Incremental | AllRight
	// 		The incremental steps required to get left up to right's most recent version
	// 		0th element is the common ancestor, ordered by birthtime, oldest first
	// 		If len() < 2, left and right are at same most recent version
	// Conflict = otherwise
	// 		nil; there is no incremental path for left to get to right's most recent version
	IncrementalPath []FilesystemVersion

	// Conflict = Incremental | AllRight: nil
	// Conflict = NoCommonAncestor: left as passed as input
	// Conflict = Diverged: contains path from left most recent common ancestor (mrca) to most
	//						recent version on left
	MRCAPathLeft []FilesystemVersion
	// Conflict = Incremental | AllRight: nil
	// Conflict = NoCommonAncestor: right as passed as input
	// Conflict = Diverged: contains path from right most recent common ancestor (mrca)
	// 						to most recent version on right
	MRCAPathRight []FilesystemVersion
}

// we must assume left and right are ordered ascendingly by ZFS_PROP_CREATETXG and that
// names are unique (bas ZFS_PROP_GUID replacement)
func MakeFilesystemDiff(left, right []FilesystemVersion) (diff FilesystemDiff) {

	if right == nil {
		panic("right must not be nil")
	}
	if left == nil {
		diff = FilesystemDiff{
			IncrementalPath: nil,
			Conflict:        ConflictAllRight,
			MRCAPathLeft:    left,
			MRCAPathRight:   right,
		}
		return
	}

	// Assert both left and right are sorted by createtxg
	{
		var leftSorted, rightSorted fsbyCreateTXG
		leftSorted = left
		rightSorted = right
		if !sort.IsSorted(leftSorted) {
			panic("cannot make filesystem diff: unsorted left")
		}
		if !sort.IsSorted(rightSorted) {
			panic("cannot make filesystem diff: unsorted right")
		}
	}

	// Find most recent common ancestor by name, preferring snapshots over bookmarks
	mrcaLeft := len(left) - 1
	var mrcaRight int
outer:
	for ; mrcaLeft >= 0; mrcaLeft-- {
		for i := len(right) - 1; i >= 0; i-- {
			if left[mrcaLeft].Guid == right[i].Guid {
				mrcaRight = i
				if i-1 >= 0 && right[i-1].Guid == right[i].Guid && right[i-1].Type == Snapshot {
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
			Conflict:        ConflictNoCommonAncestor,
			MRCAPathLeft:    left,
			MRCAPathRight:   right,
		}
		return
	}

	// diverged?
	if mrcaLeft != len(left)-1 {
		diff = FilesystemDiff{
			IncrementalPath: nil,
			Conflict:        ConflictDiverged,
			MRCAPathLeft:    left[mrcaLeft:],
			MRCAPathRight:   right[mrcaRight:],
		}
		return
	}

	if mrcaLeft != len(left)-1 {
		panic("invariant violated: mrca on left must be the last item in the left list")
	}

	// incPath must not contain bookmarks except initial one,
	//   and only if that initial bookmark's snapshot is gone
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

const ZREPL_PLACEHOLDER_PROPERTY_NAME string = "zrepl:placeholder"

type FilesystemState struct {
	Placeholder bool
	// TODO extend with resume token when that feature is finally added
}

// A somewhat efficient way to determine if a filesystem exists on this host.
// Particularly useful if exists is called more than once (will only fork exec once and cache the result)
func ZFSListFilesystemState() (localState map[string]FilesystemState, err error) {

	var actual [][]string
	if actual, err = ZFSList([]string{"name", ZREPL_PLACEHOLDER_PROPERTY_NAME}, "-t", "filesystem,volume"); err != nil {
		return
	}

	localState = make(map[string]FilesystemState, len(actual))
	for _, e := range actual {
		dp, err := NewDatasetPath(e[0])
		if err != nil {
			fmt.Errorf("ZFS does not return parseable dataset path: %s", e[0])
		}
		placeholder, _ := IsPlaceholder(dp, e[1])
		localState[e[0]] = FilesystemState{
			placeholder,
		}
	}
	return

}

// Computes the value for the ZREPL_PLACEHOLDER_PROPERTY_NAME ZFS user property
// to mark the given DatasetPath p as a placeholder
//
// We cannot simply use booleans here since user properties are always
// inherited.
//
// We hash the DatasetPath and use it to check for a given path if it is the
// one originally marked as placeholder.
//
// However, this prohibits moving datasets around via `zfs rename`. The
// placeholder attribute must be re-computed for the dataset path after the
// move.
//
// TODO better solution available?
func PlaceholderPropertyValue(p DatasetPath) string {
	ps := []byte(p.ToString())
	sum := sha512.Sum512_256(ps)
	return hex.EncodeToString(sum[:])
}

func IsPlaceholder(p DatasetPath, placeholderPropertyValue string) (isPlaceholder bool, err error) {
	expected := PlaceholderPropertyValue(p)
	isPlaceholder = expected == placeholderPropertyValue
	if !isPlaceholder {
		err = fmt.Errorf("expected %s, has %s", expected, placeholderPropertyValue)
	}
	return
}

func ZFSCreatePlaceholderFilesystem(p DatasetPath) (err error) {
	v := PlaceholderPropertyValue(p)
	cmd := exec.Command(ZFS_BINARY, "create",
		"-o", fmt.Sprintf("%s=%s", ZREPL_PLACEHOLDER_PROPERTY_NAME, v),
		"-o", "mountpoint=none",
		p.ToString())

	stderr := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderr

	if err = cmd.Start(); err != nil {
		return err
	}

	if err = cmd.Wait(); err != nil {
		err = ZFSError{
			Stderr:  stderr.Bytes(),
			WaitErr: err,
		}
	}

	return
}
