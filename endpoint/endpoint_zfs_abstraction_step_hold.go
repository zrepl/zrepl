package endpoint

import (
	"context"
	"fmt"
	"regexp"

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

// idempotently hold `version`
func HoldStep(ctx context.Context, fs string, v zfs.FilesystemVersion, jobID JobID) (Abstraction, error) {
	if !v.IsSnapshot() {
		panic(fmt.Sprintf("version must be a snapshot got %#v", v))
	}

	tag, err := StepHoldTag(jobID)
	if err != nil {
		return nil, errors.Wrap(err, "step hold tag")
	}

	if err := zfs.ZFSHold(ctx, fs, v, tag); err != nil {
		return nil, errors.Wrap(err, "step hold: zfs")
	}

	return &holdBasedAbstraction{
		Type:              AbstractionStepHold,
		FS:                fs,
		Tag:               tag,
		JobID:             jobID,
		FilesystemVersion: v,
	}, nil

}

var _ HoldExtractor = StepHoldExtractor

func StepHoldExtractor(fs *zfs.DatasetPath, v zfs.FilesystemVersion, holdTag string) Abstraction {
	if v.Type != zfs.Snapshot {
		panic("impl error")
	}

	jobID, err := ParseStepHoldTag(holdTag)
	if err == nil {
		return &holdBasedAbstraction{
			Type:              AbstractionStepHold,
			FS:                fs.ToString(),
			Tag:               holdTag,
			FilesystemVersion: v,
			JobID:             jobID,
		}
	}
	return nil
}
