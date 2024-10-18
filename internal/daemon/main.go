package daemon

import (
	"context"

	"github.com/zrepl/zrepl/internal/cli"
	"github.com/zrepl/zrepl/internal/logger"
)

type Logger = logger.Logger

var DaemonCmd = &cli.Subcommand{
	Use:   "daemon",
	Short: "run the zrepl daemon",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return Run(ctx, subcommand.Config())
	},
}
