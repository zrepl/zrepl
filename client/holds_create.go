package client

import "github.com/zrepl/zrepl/cli"

var holdsCmdCreate = &cli.Subcommand{
	Use:             "create",
	NoRequireConfig: true,
	Short:           `create zrepl-managed holds and boomkmarks (for debugging & development only!)`,
	SetupSubcommands: func() []*cli.Subcommand {
		return []*cli.Subcommand{
			holdsCmdCreateStepHold,
		}
	},
}
