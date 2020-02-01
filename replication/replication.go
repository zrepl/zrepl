// Package replication implements replication of filesystems with existing
// versions (snapshots) from a sender to a receiver.
package replication

import (
	"context"

	"github.com/zrepl/zrepl/replication/driver"
)

func Do(ctx context.Context, initialConcurrency int, planner driver.Planner) (driver.ReportFunc, driver.WaitFunc, driver.SetConcurrencyFunc) {
	return driver.Do(ctx, initialConcurrency, planner)
}
