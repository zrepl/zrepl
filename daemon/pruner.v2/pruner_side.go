package pruner

import (
	"context"

	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/zfs"
)

type SideSender struct {
	jobID endpoint.JobID
}

func NewSideSender(jid endpoint.JobID) *SideSender {
	return &SideSender{jid}
}

func (s *SideSender) isSide() Side { return nil }

var _ Side = (*SideSender)(nil)

func (s *SideSender) GetReplicationPosition(ctx context.Context, fs string) (*zfs.FilesystemVersion, error) {
	if fs == "" {
		panic("must not pass zero value for fs")
	}
	return endpoint.GetMostRecentReplicationCursorOfJob(ctx, fs, s.jobID)
}
