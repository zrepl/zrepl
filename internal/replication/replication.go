// Package replication implements replication of filesystems with existing
// versions (snapshots) from a sender to a receiver.
package replication

import (
	"context"

	"github.com/zrepl/zrepl/internal/replication/driver"
)

func Do(ctx context.Context, driverConfig driver.Config, planner driver.Planner) (driver.ReportFunc, driver.WaitFunc) {
	return driver.Do(ctx, driverConfig, planner)
}
