// zrepl replicates ZFS filesystems & volumes between pools
//
// Code Organization
//
// The cmd package uses github.com/spf13/cobra for its CLI.
//
// It combines the other packages in the zrepl project to implement zrepl functionality.
//
// Each subcommand's code is in the corresponding *.go file.
// All other *.go files contain code shared by the subcommands.
package cmd

import (
	"github.com/spf13/cobra"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

var RootCmd = &cobra.Command{
	Use:   "zrepl",
	Short: "ZFS dataset replication",
	Long: `Replicate ZFS filesystems & volumes between pools:

  - push & pull mode
  - automatic snapshot creation & pruning
  - local / over the network
  - ACLs instead of blank SSH access`,
}

var rootArgs struct {
	configFile string
}

func init() {
	//cobra.OnInitialize(initConfig)
	RootCmd.PersistentFlags().StringVar(&rootArgs.configFile, "config", "", "config file path")
}
