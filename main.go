// See cmd package.
package main

import (
	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/client"
	"github.com/zrepl/zrepl/client/status"
	"github.com/zrepl/zrepl/daemon"
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
	cli.AddSubcommand(client.MonitorCmd)
}

func main() {
	cli.Run()
}
