package snapper

import (
	"context"

	"github.com/zrepl/zrepl/daemon/job/trigger"
)

type manual struct{}

func (s *manual) Run(ctx context.Context, snapshotsTaken *trigger.Trigger) {
	// nothing to do
}

func (s *manual) Report() Report {
	return Report{Type: TypeManual, Manual: &struct{}{}}
}
