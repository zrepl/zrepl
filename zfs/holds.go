package zfs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/util/envconst"
)

// no need for feature tests, holds have been around forever

func validateNotEmpty(field, s string) error {
	if s == "" {
		return fmt.Errorf("`%s` must not be empty", field)
	}
	return nil
}

// returned err != nil is guaranteed to represent invalid hold tag
func ValidHoldTag(tag string) error {
	maxlen := envconst.Int("ZREPL_ZFS_MAX_HOLD_TAG_LEN", 256-1) // 256 include NULL byte, from module/zfs/dsl_userhold.c
	if len(tag) > maxlen {
		return fmt.Errorf("hold tag %q exceeds max length of %d", tag, maxlen)
	}
	return nil
}

// Idemptotent: does not return an error if the tag already exists
func ZFSHold(ctx context.Context, fs string, v ZFSSendArgVersion, tag string) error {
	if err := v.ValidateInMemory(fs); err != nil {
		return errors.Wrap(err, "invalid version")
	}
	if !v.IsSnapshot() {
		return errors.Errorf("can only hold snapshots, got %s", v.RelName)
	}

	if err := validateNotEmpty("tag", tag); err != nil {
		return err
	}
	fullPath := v.FullPath(fs)
	output, err := exec.CommandContext(ctx, "zfs", "hold", tag, fullPath).CombinedOutput()
	if err != nil {
		if bytes.Contains(output, []byte("tag already exists on this dataset")) {
			goto success
		}
		return &ZFSError{output, errors.Wrapf(err, "cannot hold %q", fullPath)}
	}
success:
	return nil
}

// If the snapshot does not exist, the returned error is of type *DatasetDoesNotExist
func ZFSHolds(ctx context.Context, fs, snap string) ([]string, error) {
	if err := validateZFSFilesystem(fs); err != nil {
		return nil, errors.Wrap(err, "`fs` is not a valid filesystem path")
	}
	if snap == "" {
		return nil, fmt.Errorf("`snap` must not be empty")
	}
	dp := fmt.Sprintf("%s@%s", fs, snap)
	output, err := exec.CommandContext(ctx, "zfs", "holds", "-H", dp).CombinedOutput()
	if err != nil {
		return nil, &ZFSError{output, errors.Wrap(err, "zfs holds failed")}
	}
	scan := bufio.NewScanner(bytes.NewReader(output))
	var tags []string
	for scan.Scan() {
		// NAME              TAG  TIMESTAMP
		comps := strings.SplitN(scan.Text(), "\t", 3)
		if len(comps) != 3 {
			return nil, fmt.Errorf("zfs holds: unexpected output\n%s", output)
		}
		if comps[0] != dp {
			return nil, fmt.Errorf("zfs holds: unexpected output: expecting %q as first component, got %q\n%s", dp, comps[0], output)
		}
		tags = append(tags, comps[1])
	}
	return tags, nil
}

// Idempotent: if the hold doesn't exist, this is not an error
func ZFSRelease(ctx context.Context, tag string, snaps ...string) error {
	cumLens := make([]int, len(snaps))
	for i := 1; i < len(snaps); i++ {
		cumLens[i] = cumLens[i-1] + len(snaps[i])
	}
	maxInvocationLen := 12 * os.Getpagesize()
	var noSuchTagLines, otherLines []string
	for i := 0; i < len(snaps); {
		var j = i
		for ; j < len(snaps); j++ {
			if cumLens[j]-cumLens[i] > maxInvocationLen {
				break
			}
		}
		args := []string{"release", tag}
		args = append(args, snaps[i:j]...)
		output, err := exec.CommandContext(ctx, "zfs", args...).CombinedOutput()
		if pe, ok := err.(*os.PathError); err != nil && ok && pe.Err == syscall.E2BIG {
			maxInvocationLen = maxInvocationLen / 2
			continue
		}
		maxInvocationLen = maxInvocationLen + os.Getpagesize()
		i = j

		// even if release fails for datasets where there's no hold with the tag
		// the hold is still released on datasets which have a hold with the tag
		// FIXME verify this in a platformtest
		// => screen-scrape
		scan := bufio.NewScanner(bytes.NewReader(output))
		for scan.Scan() {
			line := scan.Text()
			if strings.Contains(line, "no such tag on this dataset") {
				noSuchTagLines = append(noSuchTagLines, line)
			} else {
				otherLines = append(otherLines, line)
			}
		}

	}
	if debugEnabled {
		debug("zfs release: no such tag lines=%v otherLines=%v", noSuchTagLines, otherLines)
	}
	if len(otherLines) > 0 {
		return fmt.Errorf("unknown zfs error while releasing hold with tag %q: unidentified stderr lines\n%s", tag, strings.Join(otherLines, "\n"))
	}
	return nil
}

// Idempotent: if the hold doesn't exist, this is not an error
func ZFSReleaseAllOlderAndIncludingGUID(ctx context.Context, fs string, snapOrBookmarkGuid uint64, tag string) error {
	return doZFSReleaseAllOlderAndIncOrExcludingGUID(ctx, fs, snapOrBookmarkGuid, tag, true)
}

// Idempotent: if the hold doesn't exist, this is not an error
func ZFSReleaseAllOlderThanGUID(ctx context.Context, fs string, snapOrBookmarkGuid uint64, tag string) error {
	return doZFSReleaseAllOlderAndIncOrExcludingGUID(ctx, fs, snapOrBookmarkGuid, tag, false)
}

type zfsReleaseAllOlderAndIncOrExcludingGUIDZFSListLine struct {
	entityType EntityType
	name       string
	createtxg  uint64
	guid       uint64
	userrefs   uint64 // always 0 for bookmarks
}

func doZFSReleaseAllOlderAndIncOrExcludingGUID(ctx context.Context, fs string, snapOrBookmarkGuid uint64, tag string, includeGuid bool) error {
	// TODO channel program support still unreleased but
	// might be a huge performance improvement
	// https://github.com/zfsonlinux/zfs/pull/7902/files

	if err := validateZFSFilesystem(fs); err != nil {
		return errors.Wrap(err, "`fs` is not a valid filesystem path")
	}
	if tag == "" {
		return fmt.Errorf("`tag` must not be empty`")
	}

	output, err := exec.CommandContext(ctx,
		"zfs", "list", "-o", "type,name,createtxg,guid,userrefs",
		"-H", "-t", "snapshot,bookmark", "-r", "-d", "1", fs).CombinedOutput()
	if err != nil {
		return &ZFSError{output, errors.Wrap(err, "cannot list snapshots and their userrefs")}
	}

	lines, err := doZFSReleaseAllOlderAndIncOrExcludingGUIDParseListOutput(output)
	if err != nil {
		return errors.Wrap(err, "unexpected ZFS output")
	}

	releaseSnaps, err := doZFSReleaseAllOlderAndIncOrExcludingGUIDFindSnapshots(snapOrBookmarkGuid, includeGuid, lines)
	if err != nil {
		return err
	}

	if len(releaseSnaps) == 0 {
		return nil
	}
	return ZFSRelease(ctx, tag, releaseSnaps...)
}

func doZFSReleaseAllOlderAndIncOrExcludingGUIDParseListOutput(output []byte) ([]zfsReleaseAllOlderAndIncOrExcludingGUIDZFSListLine, error) {

	scan := bufio.NewScanner(bytes.NewReader(output))

	var lines []zfsReleaseAllOlderAndIncOrExcludingGUIDZFSListLine

	for scan.Scan() {
		const numCols = 5
		comps := strings.SplitN(scan.Text(), "\t", numCols)
		if len(comps) != numCols {
			return nil, fmt.Errorf("not %d columns\n%s", numCols, output)
		}
		dstype := comps[0]
		name := comps[1]

		var entityType EntityType
		switch dstype {
		case "snapshot":
			entityType = EntityTypeSnapshot
		case "bookmark":
			entityType = EntityTypeBookmark
		default:
			return nil, fmt.Errorf("column 0 is %q, expecting \"snapshot\" or \"bookmark\"", dstype)
		}

		createtxg, err := strconv.ParseUint(comps[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse createtxg %q: %s\n%s", comps[2], err, output)
		}

		guid, err := strconv.ParseUint(comps[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse guid %q: %s\n%s", comps[3], err, output)
		}

		var userrefs uint64
		switch entityType {
		case EntityTypeBookmark:
			if comps[4] != "-" {
				return nil, fmt.Errorf("entity type \"bookmark\" should have userrefs=\"-\", got %q", comps[4])
			}
			userrefs = 0
		case EntityTypeSnapshot:
			userrefs, err = strconv.ParseUint(comps[4], 10, 64) // shadow
			if err != nil {
				return nil, fmt.Errorf("cannot parse userrefs %q: %s\n%s", comps[4], err, output)
			}
		default:
			panic(entityType)
		}

		lines = append(lines, zfsReleaseAllOlderAndIncOrExcludingGUIDZFSListLine{
			entityType: entityType,
			name:       name,
			createtxg:  createtxg,
			guid:       guid,
			userrefs:   userrefs,
		})
	}

	return lines, nil

}

func doZFSReleaseAllOlderAndIncOrExcludingGUIDFindSnapshots(snapOrBookmarkGuid uint64, includeGuid bool, lines []zfsReleaseAllOlderAndIncOrExcludingGUIDZFSListLine) (releaseSnaps []string, err error) {

	// sort lines by createtxg,(snap < bookmark)
	// we cannot do this using zfs list -s because `type` is not a
	sort.Slice(lines, func(i, j int) (less bool) {
		if lines[i].createtxg == lines[j].createtxg {
			iET := func(t EntityType) int {
				switch t {
				case EntityTypeSnapshot:
					return 0
				case EntityTypeBookmark:
					return 1
				default:
					panic("unepxected entity type " + t.String())
				}
			}
			return iET(lines[i].entityType) < iET(lines[j].entityType)
		}
		return lines[i].createtxg < lines[j].createtxg
	})

	// iterate over snapshots oldest to newest and collect snapshots that have holds and
	// are older than (inclusive or exclusive, depends on includeGuid) a snapshot or bookmark
	// with snapOrBookmarkGuid
	foundGuid := false
	for _, line := range lines {
		if line.guid == snapOrBookmarkGuid {
			foundGuid = true
		}
		if line.userrefs > 0 {
			if !foundGuid || (foundGuid && includeGuid) {
				// only snapshots have userrefs > 0, no need to check entityType
				releaseSnaps = append(releaseSnaps, line.name)
			}
		}
		if foundGuid {
			// The secondary key in sorting (snap < bookmark) guarantees that we
			//   A) either found the snapshot with snapOrBoomkarkGuid
			//   B) or no snapshot with snapGuid exists, but one or more bookmarks of it exists
			// In the case of A, we already added the snapshot to releaseSnaps if includeGuid requests it,
			// and can ignore possible subsequent bookmarks of the snapshot.
			// In the case of B, there is nothing to add to releaseSnaps.
			break
		}
	}

	if !foundGuid {
		return nil, fmt.Errorf("cannot find snapshot or bookmark with guid %v", snapOrBookmarkGuid)
	}

	return releaseSnaps, nil
}
