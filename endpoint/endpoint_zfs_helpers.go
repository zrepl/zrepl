package endpoint

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/zfs"
)

// returns the short name (no fs# prefix)
func makeJobAndGuidBookmarkName(prefix string, fs string, guid uint64, jobid string) (string, error) {
	bmname := fmt.Sprintf(prefix+"_G_%016x_J_%s", guid, jobid)
	if err := zfs.EntityNamecheck(fmt.Sprintf("%s#%s", fs, bmname), zfs.EntityTypeBookmark); err != nil {
		return "", err
	}
	return bmname, nil
}

var jobAndGuidBookmarkRE = regexp.MustCompile(`(.+)_G_([0-9a-f]{16})_J_(.+)$`)

func parseJobAndGuidBookmarkName(fullname string, prefix string) (guid uint64, jobID JobID, _ error) {

	if len(prefix) == 0 {
		panic("prefix must not be empty")
	}

	if err := zfs.EntityNamecheck(fullname, zfs.EntityTypeBookmark); err != nil {
		return 0, JobID{}, err
	}

	_, _, name, err := zfs.DecomposeVersionString(fullname)
	if err != nil {
		return 0, JobID{}, errors.Wrap(err, "decompose bookmark name")
	}

	match := jobAndGuidBookmarkRE.FindStringSubmatch(name)
	if match == nil {
		return 0, JobID{}, errors.Errorf("bookmark name does not match regex %q", jobAndGuidBookmarkRE.String())
	}
	if match[1] != prefix {
		return 0, JobID{}, errors.Errorf("prefix component does not match: expected %q, got %q", prefix, match[1])
	}

	guid, err = strconv.ParseUint(match[2], 16, 64)
	if err != nil {
		return 0, JobID{}, errors.Wrapf(err, "parse guid component: %q", match[2])
	}

	jobID, err = MakeJobID(match[3])
	if err != nil {
		return 0, JobID{}, errors.Wrapf(err, "parse jobid component: %q", match[3])
	}

	return guid, jobID, nil
}

func destroyBookmarksOlderThan(ctx context.Context, fs string, mostRecent *zfs.ZFSSendArgVersion, jobID JobID, filter func(shortname string) (accept bool)) (destroyed []zfs.FilesystemVersion, err error) {
	if filter == nil {
		panic(filter)
	}

	fsp, err := zfs.NewDatasetPath(fs)
	if err != nil {
		return nil, errors.Wrap(err, "invalid filesystem path")
	}

	mostRecentProps, err := mostRecent.ValidateExistsAndGetCheckedProps(ctx, fs)
	if err != nil {
		return nil, errors.Wrap(err, "validate most recent version argument")
	}

	stepBookmarks, err := zfs.ZFSListFilesystemVersions(fsp, zfs.FilterFromClosure(
		func(t zfs.VersionType, name string) (accept bool, err error) {
			if t != zfs.Bookmark {
				return false, nil
			}
			return filter(name), nil
		}))
	if err != nil {
		return nil, errors.Wrap(err, "list bookmarks")
	}

	// cut off all bookmarks prior to mostRecent's CreateTXG
	var destroy []zfs.FilesystemVersion
	for _, v := range stepBookmarks {
		if v.Type != zfs.Bookmark {
			panic("implementation error")
		}
		if !filter(v.Name) {
			panic("inconsistent filter result")
		}
		if v.CreateTXG < mostRecentProps.CreateTXG {
			destroy = append(destroy, v)
		}
	}

	// FIXME use batch destroy, must adopt code to handle bookmarks
	for _, v := range destroy {
		if err := zfs.ZFSDestroyIdempotent(v.ToAbsPath(fsp)); err != nil {
			return nil, errors.Wrap(err, "destroy bookmark")
		}
	}

	return destroy, nil
}
