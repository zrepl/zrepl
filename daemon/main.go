package daemon

import (
	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/logger"
)

type Logger = logger.Logger

var DaemonCmd = &cli.Subcommand{
	Use:   "daemon",
	Short: "run the zrepl daemon",
	Run: func(subcommand *cli.Subcommand, args []string) error {
		return Run(subcommand.Config())
	},
}
