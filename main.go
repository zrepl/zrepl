// See cmd package.
package main

import (
	"github.com/zrepl/zrepl/internal/cli"
	"github.com/zrepl/zrepl/internal/client"
	"github.com/zrepl/zrepl/internal/client/status"
	"github.com/zrepl/zrepl/internal/daemon"
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
