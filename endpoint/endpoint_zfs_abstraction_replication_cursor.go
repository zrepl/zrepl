package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/zfs"
)

const replicationCursorBookmarkNamePrefix = "zrepl_CURSOR"

func ReplicationCursorBookmarkName(fs string, guid uint64, id JobID) (string, error) {
	return replicationCursorBookmarkNameImpl(fs, guid, id.String())
}

func replicationCursorBookmarkNameImpl(fs string, guid uint64, jobid string) (string, error) {
	return makeJobAndGuidBookmarkName(replicationCursorBookmarkNamePrefix, fs, guid, jobid)
}

var ErrV1ReplicationCursor = fmt.Errorf("bookmark name is a v1-replication cursor")

// err != nil always means that the bookmark is not a valid replication bookmark
//
// Returns ErrV1ReplicationCursor as error if the bookmark is a v1 replication cursor
func ParseReplicationCursorBookmarkName(fullname string) (uint64, JobID, error) {

	// check for legacy cursors
	{
		if err := zfs.EntityNamecheck(fullname, zfs.EntityTypeBookmark); err != nil {
			return 0, JobID{}, errors.Wrap(err, "parse replication cursor bookmark name")
		}
		_, _, name, err := zfs.DecomposeVersionString(fullname)
		if err != nil {
			return 0, JobID{}, errors.Wrap(err, "parse replication cursor bookmark name: decompose version string")
		}
		const V1ReplicationCursorBookmarkName = "zrepl_replication_cursor"
		if name == V1ReplicationCursorBookmarkName {
			return 0, JobID{}, ErrV1ReplicationCursor
		}
		// fallthrough to main parser
	}

	guid, jobID, err := parseJobAndGuidBookmarkName(fullname, replicationCursorBookmarkNamePrefix)
	if err != nil {
		err = errors.Wrap(err, "parse replication cursor bookmark name") // no shadow
	}
	return guid, jobID, err
}

const tentativeReplicationCursorBookmarkNamePrefix = "zrepl_CURSORTENTATIVE_"

// v must be validated by caller
func TentativeReplicationCursorBookmarkName(fs string, guid uint64, id JobID) (string, error) {
	return tentativeReplicationCursorBookmarkNameImpl(fs, guid, id.String())
}

func tentativeReplicationCursorBookmarkNameImpl(fs string, guid uint64, jobid string) (string, error) {
	return makeJobAndGuidBookmarkName(tentativeReplicationCursorBookmarkNamePrefix, fs, guid, jobid)
}

// name is the full bookmark name, including dataset path
//
// err != nil always means that the bookmark is not a step bookmark
func ParseTentativeReplicationCursorBookmarkName(fullname string) (guid uint64, jobID JobID, err error) {
	guid, jobID, err = parseJobAndGuidBookmarkName(fullname, tentativeReplicationCursorBookmarkNamePrefix)
	if err != nil {
		err = errors.Wrap(err, "parse step bookmark name") // no shadow!
	}
	return guid, jobID, err
}

// may return nil for both values, indicating there is no cursor
func GetMostRecentReplicationCursorOfJob(ctx context.Context, fs string, jobID JobID) (*zfs.FilesystemVersion, error) {
	fsp, err := zfs.NewDatasetPath(fs)
	if err != nil {
		return nil, err
	}
	candidates, err := GetReplicationCursors(ctx, fsp, jobID)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreateTXG < candidates[j].CreateTXG
	})

	mostRecent := candidates[len(candidates)-1]
	return &mostRecent, nil
}

func GetReplicationCursors(ctx context.Context, dp *zfs.DatasetPath, jobID JobID) ([]zfs.FilesystemVersion, error) {

	fs := dp.ToString()
	q := ListZFSHoldsAndBookmarksQuery{
		FS: ListZFSHoldsAndBookmarksQueryFilesystemFilter{FS: &fs},
		What: map[AbstractionType]bool{
			AbstractionReplicationCursorBookmarkV1: true,
			AbstractionReplicationCursorBookmarkV2: true,
		},
		JobID:       &jobID,
		CreateTXG:   CreateTXGRange{},
		Concurrency: 1,
	}
	abs, absErr, err := ListAbstractions(ctx, q)
	if err != nil {
		return nil, errors.Wrap(err, "get replication cursor: list bookmarks and holds")
	}
	if len(absErr) > 0 {
		return nil, ListAbstractionsErrors(absErr)
	}

	var v1, v2 []Abstraction
	for _, a := range abs {
		switch a.GetType() {
		case AbstractionReplicationCursorBookmarkV1:
			v1 = append(v1, a)
		case AbstractionReplicationCursorBookmarkV2:
			v2 = append(v2, a)
		default:
			panic("unexpected abstraction: " + a.GetType())
		}
	}

	if len(v1) > 0 {
		getLogger(ctx).WithField("bookmark", v1).
			Warn("found v1-replication cursor bookmarks, consider running migration 'replication-cursor:v1-v2' after successful replication with this zrepl version")
	}

	candidates := make([]zfs.FilesystemVersion, 0)
	for _, v := range v2 {
		candidates = append(candidates, v.GetFilesystemVersion())
	}

	return candidates, nil
}

// idempotently create a replication cursor targeting `target`
//
// returns ErrBookmarkCloningNotSupported if version is a bookmark and bookmarking bookmarks is not supported by ZFS
func CreateReplicationCursor(ctx context.Context, fs string, target zfs.FilesystemVersion, jobID JobID) (a Abstraction, err error) {
	return createBookmarkAbstraction(ctx, AbstractionReplicationCursorBookmarkV2, fs, target, jobID)
}

func CreateTentativeReplicationCursor(ctx context.Context, fs string, target zfs.FilesystemVersion, jobID JobID) (a Abstraction, err error) {
	return createBookmarkAbstraction(ctx, AbstractionTentativeReplicationCursorBookmark, fs, target, jobID)
}

func createBookmarkAbstraction(ctx context.Context, abstractionType AbstractionType, fs string, target zfs.FilesystemVersion, jobID JobID) (a Abstraction, err error) {

	bookmarkNamer := abstractionType.BookmarkNamer()
	if bookmarkNamer == nil {
		panic(abstractionType)
	}

	bookmarkname, err := bookmarkNamer(fs, target.GetGuid(), jobID)
	if err != nil {
		return nil, errors.Wrapf(err, "determine %s name", abstractionType)
	}

	// idempotently create bookmark (guid is encoded in it)

	cursorBookmark, err := zfs.ZFSBookmark(ctx, fs, target, bookmarkname)
	if err != nil {
		if err == zfs.ErrBookmarkCloningNotSupported {
			return nil, err // TODO go1.13 use wrapping
		}
		return nil, errors.Wrapf(err, "cannot create bookmark")
	}

	return &bookmarkBasedAbstraction{
		Type:              abstractionType,
		FS:                fs,
		FilesystemVersion: cursorBookmark,
		JobID:             jobID,
	}, nil
}

func ReplicationCursorV2Extractor(fs *zfs.DatasetPath, v zfs.FilesystemVersion) (_ Abstraction) {
	if v.Type != zfs.Bookmark {
		panic("impl error")
	}
	fullname := v.ToAbsPath(fs)
	guid, jobid, err := ParseReplicationCursorBookmarkName(fullname)
	if err == nil {
		if guid != v.Guid {
			// TODO log this possibly tinkered-with bookmark
			return nil
		}
		return &bookmarkBasedAbstraction{
			Type:              AbstractionReplicationCursorBookmarkV2,
			FS:                fs.ToString(),
			FilesystemVersion: v,
			JobID:             jobid,
		}
	}
	return nil
}

func ReplicationCursorV1Extractor(fs *zfs.DatasetPath, v zfs.FilesystemVersion) (_ Abstraction) {
	if v.Type != zfs.Bookmark {
		panic("impl error")
	}
	fullname := v.ToAbsPath(fs)
	_, _, err := ParseReplicationCursorBookmarkName(fullname)
	if err == ErrV1ReplicationCursor {
		return &ReplicationCursorV1{
			Type:              AbstractionReplicationCursorBookmarkV1,
			FS:                fs.ToString(),
			FilesystemVersion: v,
		}
	}
	return nil
}

var _ BookmarkExtractor = TentativeReplicationCursorExtractor

func TentativeReplicationCursorExtractor(fs *zfs.DatasetPath, v zfs.FilesystemVersion) (_ Abstraction) {
	if v.Type != zfs.Bookmark {
		panic("impl error")
	}

	fullname := v.ToAbsPath(fs)

	guid, jobid, err := ParseTentativeReplicationCursorBookmarkName(fullname)
	if guid != v.Guid {
		// TODO log this possibly tinkered-with bookmark
		return nil
	}
	if err == nil {
		bm := &bookmarkBasedAbstraction{
			Type:              AbstractionTentativeReplicationCursorBookmark,
			FS:                fs.ToString(),
			FilesystemVersion: v,
			JobID:             jobid,
		}
		return bm
	}
	return nil
}

type ReplicationCursorV1 struct {
	Type AbstractionType
	FS   string
	zfs.FilesystemVersion
}

func (c ReplicationCursorV1) GetType() AbstractionType                    { return c.Type }
func (c ReplicationCursorV1) GetFS() string                               { return c.FS }
func (c ReplicationCursorV1) GetFullPath() string                         { return fmt.Sprintf("%s#%s", c.FS, c.GetName()) }
func (c ReplicationCursorV1) GetJobID() *JobID                            { return nil }
func (c ReplicationCursorV1) GetFilesystemVersion() zfs.FilesystemVersion { return c.FilesystemVersion }
func (c ReplicationCursorV1) MarshalJSON() ([]byte, error) {
	return json.Marshal(AbstractionJSON{c})
}
func (c ReplicationCursorV1) String() string {
	return fmt.Sprintf("%s %s", c.Type, c.GetFullPath())
}
func (c ReplicationCursorV1) Destroy(ctx context.Context) error {
	if err := zfs.ZFSDestroyIdempotent(ctx, c.GetFullPath()); err != nil {
		return errors.Wrapf(err, "destroy %s %s: zfs", c.Type, c.GetFullPath())
	}
	return nil
}
func (c ReplicationCursorV1) GetDestroyDestroysVersion() bool { return true }
