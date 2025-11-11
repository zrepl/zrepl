package client

import "github.com/LyingCak3/zrepl/internal/cli"

var PprofCmd = &cli.Subcommand{
	Use: "pprof",
	SetupSubcommands: func() []*cli.Subcommand {
		return []*cli.Subcommand{PprofListenCmd, pprofActivityTraceCmd}
	},
}
