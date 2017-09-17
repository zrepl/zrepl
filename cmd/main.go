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
	"net/http"
	_ "net/http/pprof"
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
	httpPprof  string
}

func init() {
	//cobra.OnInitialize(initConfig)
	RootCmd.PersistentFlags().StringVar(&rootArgs.configFile, "config", "", "config file path")
	RootCmd.PersistentFlags().StringVar(&rootArgs.httpPprof, "debug.pprof.http", "", "run pprof http server on given port")
}

func initConfig() {

	// CPU profiling
	if rootArgs.httpPprof != "" {
		go func() {
			http.ListenAndServe(rootArgs.httpPprof, nil)
		}()
	}

	return

}
