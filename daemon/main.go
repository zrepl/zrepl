package daemon

import (
	"context"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/logger"
)

type Logger = logger.Logger

var DaemonCmd = &cli.Subcommand{
	Use:   "daemon",
	Short: "run the zrepl daemon",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return Run(ctx, subcommand.Config())
	},
}
