package daemon

import (
	"context"

	"github.com/LyingCak3/zrepl/internal/cli"
	"github.com/LyingCak3/zrepl/internal/logger"
)

type Logger = logger.Logger

var DaemonCmd = &cli.Subcommand{
	Use:   "daemon",
	Short: "run the zrepl daemon",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return Run(ctx, subcommand.Config())
	},
}
