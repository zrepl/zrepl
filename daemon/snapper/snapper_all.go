package snapper

import (
	"context"
	"fmt"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/zfs"
)

// FIXME: properly abstract snapshotting:
//   - split up things that trigger snapshotting from the mechanism
//     - timer-based trigger (periodic)
//     - call from control socket (manual)
//     - mixed modes?
//   - support a `zrepl snapshot JOBNAME` subcommand for config.SnapshottingManual
type PeriodicOrManual struct {
	s *Snapper
}

func (s *PeriodicOrManual) Run(ctx context.Context, wakeUpCommon chan<- struct{}) {
	if s.s != nil {
		s.s.Run(ctx, wakeUpCommon)
	}
}

// Returns nil if manual
func (s *PeriodicOrManual) Report() *Report {
	if s.s != nil {
		return s.s.Report()
	}
	return nil
}

func FromConfig(g *config.Global, fsf zfs.DatasetFilter, in config.SnapshottingEnum) (*PeriodicOrManual, error) {
	switch v := in.Ret.(type) {
	case *config.SnapshottingPeriodic:
		snapper, err := PeriodicFromConfig(g, fsf, v)
		if err != nil {
			return nil, err
		}
		return &PeriodicOrManual{snapper}, nil
	case *config.SnapshottingManual:
		return &PeriodicOrManual{}, nil
	default:
		return nil, fmt.Errorf("unknown snapshotting type %T", v)
	}
}
