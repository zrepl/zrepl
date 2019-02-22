// Package replication implements replication of filesystems with existing
// versions (snapshots) from a sender to a receiver.
package replication

import (
	"context"

	"github.com/zrepl/zrepl/replication/driver"
)

func Do(ctx context.Context, planner driver.Planner) (driver.ReportFunc, driver.WaitFunc) {
	return driver.Do(ctx, planner)
}
