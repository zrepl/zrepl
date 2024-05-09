package snapper

import (
	"context"
	"fmt"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/job/trigger"
	"github.com/zrepl/zrepl/zfs"
)

type Type string

const (
	TypePeriodic Type = "periodic"
	TypeCron     Type = "cron"
	TypeManual   Type = "manual"
)

type Snapper interface {
	Run(ctx context.Context, snapshotsTaken *trigger.Manual)
	Report() Report
}

type Report struct {
	Type     Type
	Periodic *PeriodicReport
	Cron     *CronReport
	Manual   *struct{}
}

func FromConfig(g *config.Global, fsf zfs.DatasetFilter, in config.SnapshottingEnum) (Snapper, error) {
	switch v := in.Ret.(type) {
	case *config.SnapshottingPeriodic:
		return periodicFromConfig(g, fsf, v)
	case *config.SnapshottingCron:
		return cronFromConfig(fsf, *v)
	case *config.SnapshottingManual:
		return &manual{}, nil
	default:
		return nil, fmt.Errorf("unknown snapshotting type %T", v)
	}
}
