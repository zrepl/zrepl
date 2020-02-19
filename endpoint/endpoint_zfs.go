package endpoint

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	"github.com/kr/pretty"
	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/zfs"
)

var stepHoldTagRE = regexp.MustCompile("^zrepl_STEP_J_(.+)")

func StepHoldTag(jobid JobID) (string, error) {
	return stepHoldTagImpl(jobid.String())
}

func stepHoldTagImpl(jobid string) (string, error) {
	t := fmt.Sprintf("zrepl_STEP_J_%s", jobid)
	if err := zfs.ValidHoldTag(t); err != nil {
		return "", err
	}
	return t, nil
}

// err != nil always means that the bookmark is not a step bookmark
func ParseStepHoldTag(tag string) (JobID, error) {
	match := stepHoldTagRE.FindStringSubmatch(tag)
	if match == nil {
		return JobID{}, fmt.Errorf("parse hold tag: match regex %q", stepHoldTagRE)
	}
	jobID, err := MakeJobID(match[1])
	if err != nil {
		return JobID{}, errors.Wrap(err, "parse hold tag: invalid job id field")
	}
	return jobID, nil
}

const stepBookmarkNamePrefix = "zrepl_STEP"

// v must be validated by caller
func StepBookmarkName(fs string, guid uint64, id JobID) (string, error) {
	return stepBookmarkNameImpl(fs, guid, id.String())
}

func stepBookmarkNameImpl(fs string, guid uint64, jobid string) (string, error) {
	return makeJobAndGuidBookmarkName(stepBookmarkNamePrefix, fs, guid, jobid)
}

// name is the full bookmark name, including dataset path
//
// err != nil always means that the bookmark is not a step bookmark
func ParseStepBookmarkName(fullname string) (guid uint64, jobID JobID, err error) {
	guid, jobID, err = parseJobAndGuidBookmarkName(fullname, stepBookmarkNamePrefix)
	if err != nil {
		err = errors.Wrap(err, "parse step bookmark name") // no shadow!
	}
	return guid, jobID, err
}

const replicationCursorBookmarkNamePrefix = "zrepl_CURSOR"

func ReplicationCursorBookmarkName(fs string, guid uint64, id JobID) (string, error) {
	return replicationCursorBookmarkNameImpl(fs, guid, id.String())
}

func replicationCursorBookmarkNameImpl(fs string, guid uint64, jobid string) (string, error) {
	return makeJobAndGuidBookmarkName(replicationCursorBookmarkNamePrefix, fs, guid, jobid)
}

var ErrV1ReplicationCursor = fmt.Errorf("bookmark name is a v1-replication cursor")

//err != nil always means that the bookmark is not a valid replication bookmark
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
	}

	guid, jobID, err := parseJobAndGuidBookmarkName(fullname, replicationCursorBookmarkNamePrefix)
	if err != nil {
		err = errors.Wrap(err, "parse replication cursor bookmark name") // no shadow
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

func GetReplicationCursors(ctx context.Context, fs *zfs.DatasetPath, jobID JobID) ([]zfs.FilesystemVersion, error) {

	listOut := &ListHoldsAndBookmarksOutput{}
	if err := listZFSHoldsAndBookmarksImplFS(ctx, listOut, fs); err != nil {
		return nil, errors.Wrap(err, "get replication cursor: list bookmarks and holds")
	}

	if len(listOut.V1ReplicationCursors) > 0 {
		getLogger(ctx).WithField("bookmark", pretty.Sprint(listOut.V1ReplicationCursors)).
			Warn("found v1-replication cursor bookmarks, consider running migration 'replication-cursor:v1-v2' after successful replication with this zrepl version")
	}

	candidates := make([]zfs.FilesystemVersion, 0)
	for _, v := range listOut.ReplicationCursorBookmarks {
		zv := zfs.ZFSSendArgVersion{
			RelName: "#" + v.Name,
			GUID:    v.Guid,
		}
		if err := zv.ValidateExists(ctx, v.FS); err != nil {
			getLogger(ctx).WithError(err).WithField("bookmark", zv.FullPath(v.FS)).
				Error("found invalid replication cursor bookmark")
			continue
		}
		candidates = append(candidates, v.v)
	}

	return candidates, nil
}

// `target` is validated before replication cursor is set. if validation fails, the cursor is not moved.
//
// returns ErrBookmarkCloningNotSupported if version is a bookmark and bookmarking bookmarks is not supported by ZFS
func MoveReplicationCursor(ctx context.Context, fs string, target *zfs.ZFSSendArgVersion, jobID JobID) (destroyedCursors []zfs.FilesystemVersion, err error) {

	if !target.IsSnapshot() {
		return nil, zfs.ErrBookmarkCloningNotSupported
	}

	snapProps, err := target.ValidateExistsAndGetCheckedProps(ctx, fs)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid replication cursor target %q (guid=%v)", target.RelName, target.GUID)
	}

	bookmarkname, err := ReplicationCursorBookmarkName(fs, snapProps.Guid, jobID)
	if err != nil {
		return nil, errors.Wrap(err, "determine replication cursor name")
	}

	// idempotently create bookmark (guid is encoded in it, hence we'll most likely add a new one
	// cleanup the old one afterwards

	err = zfs.ZFSBookmark(fs, *target, bookmarkname)
	if err != nil {
		if err == zfs.ErrBookmarkCloningNotSupported {
			return nil, err // TODO go1.13 use wrapping
		}
		return nil, errors.Wrapf(err, "cannot create bookmark")
	}

	destroyedCursors, err = DestroyObsoleteReplicationCursors(ctx, fs, target, jobID)
	if err != nil {
		return nil, errors.Wrap(err, "destroy obsolete replication cursors")
	}

	return destroyedCursors, nil
}

func DestroyObsoleteReplicationCursors(ctx context.Context, fs string, target *zfs.ZFSSendArgVersion, jobID JobID) (destroyed []zfs.FilesystemVersion, err error) {
	return destroyBookmarksOlderThan(ctx, fs, target, jobID, func(shortname string) (accept bool) {
		_, parsedID, err := ParseReplicationCursorBookmarkName(fs + "#" + shortname)
		return err == nil && parsedID == jobID
	})
}

// idempotently hold / step-bookmark `version`
//
// returns ErrBookmarkCloningNotSupported if version is a bookmark and bookmarking bookmarks is not supported by ZFS
func HoldStep(ctx context.Context, fs string, v *zfs.ZFSSendArgVersion, jobID JobID) error {
	if err := v.ValidateExists(ctx, fs); err != nil {
		return err
	}
	if v.IsSnapshot() {

		tag, err := StepHoldTag(jobID)
		if err != nil {
			return errors.Wrap(err, "step hold tag")
		}

		if err := zfs.ZFSHold(ctx, fs, *v, tag); err != nil {
			return errors.Wrap(err, "step hold: zfs")
		}

		return nil
	}

	v.MustBeBookmark()

	bmname, err := StepBookmarkName(fs, v.GUID, jobID)
	if err != nil {
		return errors.Wrap(err, "create step bookmark: determine bookmark name")
	}
	// idempotently create bookmark
	err = zfs.ZFSBookmark(fs, *v, bmname)
	if err != nil {
		if err == zfs.ErrBookmarkCloningNotSupported {
			// TODO we could actually try to find a local snapshot that has the requested GUID
			// 		however, the replication algorithm prefers snapshots anyways, so this quest
			// 		is most likely not going to be successful. Also, there's the possibility that
			//      the caller might want to filter what snapshots are eligibile, and this would
			//      complicate things even further.
			return err // TODO go1.13 use wrapping
		}
		return errors.Wrap(err, "create step bookmark: zfs")
	}
	return nil
}

// idempotently release the step-hold on v if v is a snapshot
// or idempotently destroy the step-bookmark  of v if v is a bookmark
//
// note that this operation leaves v itself untouched, unless v is the step-bookmark itself, in which case v is destroyed
//
// returns an instance of *zfs.DatasetDoesNotExist if `v` does not exist
func ReleaseStep(ctx context.Context, fs string, v *zfs.ZFSSendArgVersion, jobID JobID) error {

	if err := v.ValidateExists(ctx, fs); err != nil {
		return err
	}

	if v.IsSnapshot() {
		tag, err := StepHoldTag(jobID)
		if err != nil {
			return errors.Wrap(err, "step release tag")
		}

		if err := zfs.ZFSRelease(ctx, tag, v.FullPath(fs)); err != nil {
			return errors.Wrap(err, "step release: zfs")
		}

		return nil
	}

	v.MustBeBookmark()

	bmname, err := StepBookmarkName(fs, v.GUID, jobID)
	if err != nil {
		return errors.Wrap(err, "step release: determine bookmark name")
	}
	// idempotently destroy bookmark

	if err := zfs.ZFSDestroyIdempotent(bmname); err != nil {
		return errors.Wrap(err, "step release: bookmark destroy: zfs")
	}

	return nil
}

// release {step holds, step bookmarks} earlier and including `mostRecent`
func ReleaseStepAll(ctx context.Context, fs string, mostRecent *zfs.ZFSSendArgVersion, jobID JobID) error {

	if err := mostRecent.ValidateInMemory(fs); err != nil {
		return err
	}

	tag, err := StepHoldTag(jobID)
	if err != nil {
		return errors.Wrap(err, "step release all: tag")
	}

	err = zfs.ZFSReleaseAllOlderAndIncludingGUID(ctx, fs, mostRecent.GUID, tag)
	if err != nil {
		return errors.Wrapf(err, "step release all: release holds older and including %q", mostRecent.FullPath(fs))
	}

	_, err = destroyBookmarksOlderThan(ctx, fs, mostRecent, jobID, func(shortname string) bool {
		_, parsedId, parseErr := ParseStepBookmarkName(fs + "#" + shortname)
		return parseErr == nil && parsedId == jobID
	})
	if err != nil {
		return errors.Wrapf(err, "step release all: destroy bookmarks older than %q", mostRecent.FullPath(fs))
	}

	return nil
}

var lastReceivedHoldTagRE = regexp.MustCompile("^zrepl_last_received_J_(.+)$")

// err != nil always means that the bookmark is not a step bookmark
func ParseLastReceivedHoldTag(tag string) (JobID, error) {
	match := lastReceivedHoldTagRE.FindStringSubmatch(tag)
	if match == nil {
		return JobID{}, errors.Errorf("parse last-received-hold tag: does not match regex %s", lastReceivedHoldTagRE.String())
	}
	jobId, err := MakeJobID(match[1])
	if err != nil {
		return JobID{}, errors.Wrap(err, "parse last-received-hold tag: invalid job id field")
	}
	return jobId, nil
}

func LastReceivedHoldTag(jobID JobID) (string, error) {
	return lastReceivedHoldImpl(jobID.String())
}

func lastReceivedHoldImpl(jobid string) (string, error) {
	tag := fmt.Sprintf("zrepl_last_received_J_%s", jobid)
	if err := zfs.ValidHoldTag(tag); err != nil {
		return "", err
	}
	return tag, nil
}

func MoveLastReceivedHold(ctx context.Context, fs string, to zfs.ZFSSendArgVersion, jobID JobID) error {
	if err := to.ValidateExists(ctx, fs); err != nil {
		return err
	}
	if err := zfs.EntityNamecheck(to.FullPath(fs), zfs.EntityTypeSnapshot); err != nil {
		return err
	}

	tag, err := LastReceivedHoldTag(jobID)
	if err != nil {
		return errors.Wrap(err, "last-received-hold: hold tag")
	}

	// we never want to be without a hold
	// => hold new one before releasing old hold

	err = zfs.ZFSHold(ctx, fs, to, tag)
	if err != nil {
		return errors.Wrap(err, "last-received-hold: hold newly received")
	}

	err = zfs.ZFSReleaseAllOlderThanGUID(ctx, fs, to.GUID, tag)
	if err != nil {
		return errors.Wrap(err, "last-received-hold: release older holds")
	}

	return nil
}

type ListHoldsAndBookmarksOutputBookmarkV1ReplicationCursor struct {
	FS   string
	Name string
}

type ListHoldsAndBookmarksOutput struct {
	StepBookmarks []*ListHoldsAndBookmarksOutputBookmark
	StepHolds     []*ListHoldsAndBookmarksOutputHold

	ReplicationCursorBookmarks []*ListHoldsAndBookmarksOutputBookmark
	V1ReplicationCursors       []*ListHoldsAndBookmarksOutputBookmarkV1ReplicationCursor
	LastReceivedHolds          []*ListHoldsAndBookmarksOutputHold
}

type ListHoldsAndBookmarksOutputBookmark struct {
	FS, Name string
	Guid     uint64
	JobID    JobID
	v        zfs.FilesystemVersion
}

type ListHoldsAndBookmarksOutputHold struct {
	FS            string
	Snap          string
	SnapGuid      uint64
	SnapCreateTXG uint64
	Tag           string
	JobID         JobID
}

// List all holds and bookmarks managed by endpoint
func ListZFSHoldsAndBookmarks(ctx context.Context, fsfilter zfs.DatasetFilter) (*ListHoldsAndBookmarksOutput, error) {

	// initialize all fields so that JSON serializion of output looks pretty (see client/holds.go)
	// however, listZFSHoldsAndBookmarksImplFS shouldn't rely on it
	out := &ListHoldsAndBookmarksOutput{
		StepBookmarks:              make([]*ListHoldsAndBookmarksOutputBookmark, 0),
		StepHolds:                  make([]*ListHoldsAndBookmarksOutputHold, 0),
		ReplicationCursorBookmarks: make([]*ListHoldsAndBookmarksOutputBookmark, 0),
		V1ReplicationCursors:       make([]*ListHoldsAndBookmarksOutputBookmarkV1ReplicationCursor, 0),
		LastReceivedHolds:          make([]*ListHoldsAndBookmarksOutputHold, 0),
	}

	fss, err := zfs.ZFSListMapping(ctx, fsfilter)
	if err != nil {
		return nil, errors.Wrap(err, "list filesystems")
	}

	for _, fs := range fss {
		err := listZFSHoldsAndBookmarksImplFS(ctx, out, fs)
		if err != nil {
			// FIXME if _, ok := err.(*zfs.DatasetDoesNotExist); ok { noop }
			return nil, errors.Wrapf(err, "list holds and bookmarks on %q", fs.ToString())
		}
	}
	return out, nil
}

func listZFSHoldsAndBookmarksImplFS(ctx context.Context, out *ListHoldsAndBookmarksOutput, fs *zfs.DatasetPath) error {
	fsvs, err := zfs.ZFSListFilesystemVersions(fs, nil)
	if err != nil {
		// FIXME if _, ok := err.(*zfs.DatasetDoesNotExist); ok { noop }
		return errors.Wrapf(err, "list filesystem versions of %q", fs.ToString())
	}
	for _, v := range fsvs {
		switch v.Type {
		case zfs.Bookmark:
			listZFSHoldsAndBookmarksImplTryParseBookmark(ctx, out, fs, v)
		case zfs.Snapshot:
			holds, err := zfs.ZFSHolds(ctx, fs.ToString(), v.Name)
			if err != nil {
				if _, ok := err.(*zfs.DatasetDoesNotExist); ok {
					holds  = []string{}
					// fallthrough
				} else {
					return errors.Wrapf(err, "get holds of %q", v.ToAbsPath(fs))
				}
			}
			for _, tag := range holds {
				listZFSHoldsAndBookmarksImplSnapshotTryParseHold(ctx, out, fs, v, tag)
			}
		default:
			continue
		}
	}
	return nil
}

// pure function, err != nil always indicates parsing error
func listZFSHoldsAndBookmarksImplTryParseBookmark(ctx context.Context, out *ListHoldsAndBookmarksOutput, fs *zfs.DatasetPath, v zfs.FilesystemVersion) {
	var err error

	if v.Type != zfs.Bookmark {
		panic("impl error")
	}

	fullname := v.ToAbsPath(fs)

	bm := &ListHoldsAndBookmarksOutputBookmark{
		FS: fs.ToString(), Name: v.Name, v: v,
	}
	bm.Guid, bm.JobID, err = ParseStepBookmarkName(fullname)
	if err == nil {
		out.StepBookmarks = append(out.StepBookmarks, bm)
		return
	}

	bm.Guid, bm.JobID, err = ParseReplicationCursorBookmarkName(fullname)
	if err == nil {
		out.ReplicationCursorBookmarks = append(out.ReplicationCursorBookmarks, bm)
		return
	} else if err == ErrV1ReplicationCursor {
		v1rc := &ListHoldsAndBookmarksOutputBookmarkV1ReplicationCursor{
			FS: fs.ToString(), Name: v.Name,
		}
		out.V1ReplicationCursors = append(out.V1ReplicationCursors, v1rc)
		return
	}
}

// pure function, err != nil always indicates parsing error
func listZFSHoldsAndBookmarksImplSnapshotTryParseHold(ctx context.Context, out *ListHoldsAndBookmarksOutput, fs *zfs.DatasetPath, v zfs.FilesystemVersion, holdTag string) {
	var err error

	if v.Type != zfs.Snapshot {
		panic("impl error")
	}

	hold := &ListHoldsAndBookmarksOutputHold{
		FS:            fs.ToString(),
		Snap:          v.Name,
		SnapGuid:      v.Guid,
		SnapCreateTXG: v.CreateTXG,
		Tag:           holdTag,
	}
	hold.JobID, err = ParseStepHoldTag(holdTag)
	if err == nil {
		out.StepHolds = append(out.StepHolds, hold)
		return
	}

	hold.JobID, err = ParseLastReceivedHoldTag(holdTag)
	if err == nil {
		out.LastReceivedHolds = append(out.LastReceivedHolds, hold)
		return
	}

}
