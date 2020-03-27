package endpoint

import (
	"context"
	"fmt"
	"regexp"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/util/errorarray"
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

// idempotently hold / step-bookmark `version`
//
// returns ErrBookmarkCloningNotSupported if version is a bookmark and bookmarking bookmarks is not supported by ZFS
func HoldStep(ctx context.Context, fs string, v zfs.FilesystemVersion, jobID JobID) error {
	if v.IsSnapshot() {

		tag, err := StepHoldTag(jobID)
		if err != nil {
			return errors.Wrap(err, "step hold tag")
		}

		if err := zfs.ZFSHold(ctx, fs, v, tag); err != nil {
			return errors.Wrap(err, "step hold: zfs")
		}

		return nil
	}

	if !v.IsBookmark() {
		panic(fmt.Sprintf("version must bei either snapshot or bookmark, got %#v", v))
	}

	bmname, err := StepBookmarkName(fs, v.Guid, jobID)
	if err != nil {
		return errors.Wrap(err, "create step bookmark: determine bookmark name")
	}
	// idempotently create bookmark
	err = zfs.ZFSBookmark(ctx, fs, v.ToSendArgVersion(), bmname)
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
func ReleaseStep(ctx context.Context, fs string, v zfs.FilesystemVersion, jobID JobID) error {

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
	if !v.IsBookmark() {
		panic(fmt.Sprintf("impl error: expecting version to be a bookmark, got %#v", v))
	}

	bmname, err := StepBookmarkName(fs, v.Guid, jobID)
	if err != nil {
		return errors.Wrap(err, "step release: determine bookmark name")
	}
	// idempotently destroy bookmark

	if err := zfs.ZFSDestroyIdempotent(ctx, bmname); err != nil {
		return errors.Wrap(err, "step release: bookmark destroy: zfs")
	}

	return nil
}

// release {step holds, step bookmarks} earlier and including `mostRecent`
func ReleaseStepCummulativeInclusive(ctx context.Context, fs string, mostRecent zfs.FilesystemVersion, jobID JobID) error {
	q := ListZFSHoldsAndBookmarksQuery{
		What: AbstractionTypeSet{
			AbstractionStepHold:     true,
			AbstractionStepBookmark: true,
		},
		FS: ListZFSHoldsAndBookmarksQueryFilesystemFilter{
			FS: &fs,
		},
		JobID: &jobID,
		Until: &InclusiveExclusiveCreateTXG{
			CreateTXG: mostRecent.CreateTXG,
			Inclusive: &zfs.NilBool{B: true},
		},
		Concurrency: 1,
	}
	abs, absErrs, err := ListAbstractions(ctx, q)
	if err != nil {
		return errors.Wrap(err, "step release cummulative: list")
	}
	if len(absErrs) > 0 {
		return errors.Wrap(ListAbstractionsErrors(absErrs), "step release cummulative: list")
	}

	getLogger(ctx).WithField("step_holds_and_bookmarks", fmt.Sprintf("%s", abs)).Debug("releasing step holds and bookmarks")

	var errs []error
	for res := range BatchDestroy(ctx, abs) {
		log := getLogger(ctx).
			WithField("step_hold_or_bookmark", res.Abstraction)
		if res.DestroyErr != nil {
			errs = append(errs, res.DestroyErr)
			log.WithError(err).
				Error("cannot release step hold or bookmark")
		} else {
			log.Info("released step hold or bookmark")
		}
	}
	if len(errs) == 0 {
		return nil
	} else {
		return errorarray.Wrap(errs, "step release cummulative: release")
	}
}

func TryReleaseStepStaleFS(ctx context.Context, fs string, jobID JobID) {

	q := ListZFSHoldsAndBookmarksQuery{
		FS: ListZFSHoldsAndBookmarksQueryFilesystemFilter{
			FS: &fs,
		},
		JobID: &jobID,
		What: AbstractionTypeSet{
			AbstractionStepHold:     true,
			AbstractionStepBookmark: true,
		},
		Concurrency: 1,
	}
	staleness, err := ListStale(ctx, q)
	if err != nil {
		getLogger(ctx).WithError(err).Error("cannot list stale step holds and bookmarks")
		return
	}
	for _, s := range staleness.Stale {
		getLogger(ctx).WithField("stale_step_hold_or_bookmark", s).Info("batch-destroying stale step hold or bookmark")
	}
	for res := range BatchDestroy(ctx, staleness.Stale) {
		if res.DestroyErr != nil {
			getLogger(ctx).
				WithField("stale_step_hold_or_bookmark", res.Abstraction).
				WithError(res.DestroyErr).
				Error("cannot destroy stale step-hold or bookmark")
		} else {
			getLogger(ctx).
				WithField("stale_step_hold_or_bookmark", res.Abstraction).
				WithError(res.DestroyErr).
				Info("destroyed stale step-hold or bookmark")
		}
	}

}

var _ BookmarkExtractor = StepBookmarkExtractor

func StepBookmarkExtractor(fs *zfs.DatasetPath, v zfs.FilesystemVersion) (_ Abstraction) {
	if v.Type != zfs.Bookmark {
		panic("impl error")
	}

	fullname := v.ToAbsPath(fs)

	guid, jobid, err := ParseStepBookmarkName(fullname)
	if guid != v.Guid {
		// TODO log this possibly tinkered-with bookmark
		return nil
	}
	if err == nil {
		bm := &ListHoldsAndBookmarksOutputBookmark{
			Type:              AbstractionStepBookmark,
			FS:                fs.ToString(),
			FilesystemVersion: v,
			JobID:             jobid,
		}
		return bm
	}
	return nil
}

var _ HoldExtractor = StepHoldExtractor

func StepHoldExtractor(fs *zfs.DatasetPath, v zfs.FilesystemVersion, holdTag string) Abstraction {
	if v.Type != zfs.Snapshot {
		panic("impl error")
	}

	jobID, err := ParseStepHoldTag(holdTag)
	if err == nil {
		return &ListHoldsAndBookmarksOutputHold{
			Type:              AbstractionStepHold,
			FS:                fs.ToString(),
			Tag:               holdTag,
			FilesystemVersion: v,
			JobID:             jobID,
		}
	}
	return nil
}
