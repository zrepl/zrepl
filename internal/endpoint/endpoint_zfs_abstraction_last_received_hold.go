package endpoint

import (
	"context"
	"fmt"
	"regexp"

	"github.com/pkg/errors"

	"github.com/LyingCak3/zrepl/internal/zfs"
)

const (
	LastReceivedHoldTagNamePrefix = "zrepl_last_received_J_"
)

var lastReceivedHoldTagRE = regexp.MustCompile("^zrepl_last_received_J_(.+)$")

var _ HoldExtractor = LastReceivedHoldExtractor

func LastReceivedHoldExtractor(fs *zfs.DatasetPath, v zfs.FilesystemVersion, holdTag string) Abstraction {
	var err error

	if v.Type != zfs.Snapshot {
		panic("impl error")
	}

	jobID, err := ParseLastReceivedHoldTag(holdTag)
	if err == nil {
		return &holdBasedAbstraction{
			Type:              AbstractionLastReceivedHold,
			FS:                fs.ToString(),
			FilesystemVersion: v,
			Tag:               holdTag,
			JobID:             jobID,
		}
	}
	return nil
}

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
	tag := fmt.Sprintf("%s%s", LastReceivedHoldTagNamePrefix, jobid)
	if err := zfs.ValidHoldTag(tag); err != nil {
		return "", err
	}
	return tag, nil
}

func CreateLastReceivedHold(ctx context.Context, fs string, to zfs.FilesystemVersion, jobID JobID) (Abstraction, error) {

	if !to.IsSnapshot() {
		return nil, errors.Errorf("last-received-hold: target must be a snapshot: %s", to.FullPath(fs))
	}

	tag, err := LastReceivedHoldTag(jobID)
	if err != nil {
		return nil, errors.Wrap(err, "last-received-hold: hold tag")
	}

	// we never want to be without a hold
	// => hold new one before releasing old hold

	err = zfs.ZFSHold(ctx, fs, to, tag)
	if err != nil {
		return nil, errors.Wrap(err, "last-received-hold: hold newly received")
	}

	return &holdBasedAbstraction{
		Type:              AbstractionLastReceivedHold,
		FS:                fs,
		FilesystemVersion: to,
		JobID:             jobID,
		Tag:               tag,
	}, nil
}
