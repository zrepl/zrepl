// See cmd package.
package main

import (
	"github.com/LyingCak3/zrepl/internal/cli"
	"github.com/LyingCak3/zrepl/internal/client"
	"github.com/LyingCak3/zrepl/internal/client/status"
	"github.com/LyingCak3/zrepl/internal/daemon"
)

func init() {
	cli.AddSubcommand(daemon.DaemonCmd)
	cli.AddSubcommand(status.Subcommand)
	cli.AddSubcommand(client.SignalCmd)
	cli.AddSubcommand(client.StdinserverCmd)
	cli.AddSubcommand(client.ConfigcheckCmd)
	cli.AddSubcommand(client.VersionCmd)
	cli.AddSubcommand(client.PprofCmd)
	cli.AddSubcommand(client.TestCmd)
	cli.AddSubcommand(client.MigrateCmd)
	cli.AddSubcommand(client.ZFSAbstractionsCmd)
}

func main() {
	cli.Run()
}
