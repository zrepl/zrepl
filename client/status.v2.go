package client

import (
	"context"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/client/status.v2"
	"github.com/zrepl/zrepl/config"
)

var StatusV2Command = &cli.Subcommand{
	Use:   "status-v2",
	Short: "start status-v2 terminal UI",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return runStatusV2Command(ctx, subcommand.Config(), args)
	},
}

func runStatusV2Command(ctx context.Context, config *config.Config, args []string) error {
	return status.Main(config, args)
}
