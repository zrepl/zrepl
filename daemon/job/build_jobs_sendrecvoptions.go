package job

import (
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/zfs"
)

type SendingJobConfig interface {
	GetFilesystems() config.FilesystemsFilter
	GetSendOptions() *config.SendOptions // must not be nil
}

func buildSenderConfig(in SendingJobConfig, jobID endpoint.JobID) (*endpoint.SenderConfig, error) {

	fsf, err := filters.DatasetMapFilterFromConfig(in.GetFilesystems())
	if err != nil {
		return nil, errors.Wrap(err, "cannot build filesystem filter")
	}

	return &endpoint.SenderConfig{
		FSF:     fsf,
		Encrypt: &zfs.NilBool{B: in.GetSendOptions().Encrypted},
		JobID:   jobID,
	}, nil
}

type ReceivingJobConfig interface {
	GetRootFS() string
	GetAppendClientIdentity() bool
	GetRecvOptions() *config.RecvOptions
}

func buildReceiverConfig(in ReceivingJobConfig, jobID endpoint.JobID) (rc endpoint.ReceiverConfig, err error) {
	rootFs, err := zfs.NewDatasetPath(in.GetRootFS())
	if err != nil {
		return rc, errors.New("root_fs is not a valid zfs filesystem path")
	}
	if rootFs.Length() <= 0 {
		return rc, errors.New("root_fs must not be empty") // duplicates error check of receiver
	}

	rc = endpoint.ReceiverConfig{
		JobID:                      jobID,
		RootWithoutClientComponent: rootFs,
		AppendClientIdentity:       in.GetAppendClientIdentity(),
	}
	if err := rc.Validate(); err != nil {
		return rc, errors.Wrap(err, "cannot build receiver config")
	}

	return rc, nil
}
