// See cmd package.
package main

import (
	"log"
	"os"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/daemon"
	"github.com/zrepl/zrepl/cmd/config"
)

var rootCmd = &cobra.Command{
	Use:   "zrepl",
	Short: "ZFS dataset replication",
	Long: `Replicate ZFS filesystems & volumes between pools:

  - push & pull mode
  - automatic snapshot creation & pruning
  - local / over the network
  - ACLs instead of blank SSH access`,
}


var daemonCmd = &cobra.Command{
	Use: "daemon",
	Short: "daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		conf, err := config.ParseConfig(rootArgs.configFile)
		if err != nil {
			return err
		}
		return daemon.Run(conf)
	},
}

var rootArgs struct {
	configFile string
}

func init() {
	//cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&rootArgs.configFile, "config", "", "config file path")
	rootCmd.AddCommand(daemonCmd)
}

func main() {


	if err := rootCmd.Execute(); err != nil {
		log.Printf("error executing root command: %s", err)
		os.Exit(1)
	}
}
