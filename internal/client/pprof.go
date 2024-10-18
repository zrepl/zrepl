package client

import "github.com/zrepl/zrepl/internal/cli"

var PprofCmd = &cli.Subcommand{
	Use: "pprof",
	SetupSubcommands: func() []*cli.Subcommand {
		return []*cli.Subcommand{PprofListenCmd, pprofActivityTraceCmd}
	},
}
