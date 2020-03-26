package client

import "github.com/zrepl/zrepl/cli"

var zabsCmdCreate = &cli.Subcommand{
	Use:             "create",
	NoRequireConfig: true,
	Short:           `create zrepl ZFS abstractions (mostly useful for debugging & development, users should not need to use this command)`,
	SetupSubcommands: func() []*cli.Subcommand {
		return []*cli.Subcommand{
			zabsCmdCreateStepHold,
		}
	},
}
