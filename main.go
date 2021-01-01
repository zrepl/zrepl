// See cmd package.
package main

import (
	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/client"
	statusv2 "github.com/zrepl/zrepl/client/status.v2"
	"github.com/zrepl/zrepl/daemon"
)

func init() {
	cli.AddSubcommand(daemon.DaemonCmd)
	cli.AddSubcommand(client.StatusCmd)
	cli.AddSubcommand(statusv2.Subcommand)
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
