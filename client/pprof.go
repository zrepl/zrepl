package client

import "github.com/zrepl/zrepl/cli"

var PprofCmd = &cli.Subcommand{
	Use: "pprof",
	SetupSubcommands: func() []*cli.Subcommand {
		return []*cli.Subcommand{PprofListenCmd, pprofActivityTraceCmd}
	},
}
