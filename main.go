// See cmd package.
package main

import (
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/client"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"log"
	"os"
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
	Use:   "daemon",
	Short: "daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		conf, err := config.ParseConfig(rootArgs.configFile)
		if err != nil {
			return err
		}
		return daemon.Run(conf)
	},
}

var wakeupCmd = &cobra.Command{
	Use:   "wakeup",
	Short: "wake up jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		conf, err := config.ParseConfig(rootArgs.configFile)
		if err != nil {
			return err
		}
		return client.RunWakeup(conf, args)
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "status",
	RunE: func(cmd *cobra.Command, args []string) error {
		conf, err := config.ParseConfig(rootArgs.configFile)
		if err != nil {
			return err
		}
		return client.RunStatus(conf, args)
	},
}

var stdinserverCmd = &cobra.Command{
	Use:   "stdinserver CLIENT_IDENTITY",
	Short: "start in stdinserver mode (from authorized_keys file)",
	RunE: func(cmd *cobra.Command, args []string) error {
		conf, err := config.ParseConfig(rootArgs.configFile)
		if err != nil {
			return err
		}
		return client.RunStdinserver(conf, args)
	},
}

var rootArgs struct {
	configFile string
}

func init() {
	//cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&rootArgs.configFile, "config", "", "config file path")
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(wakeupCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(stdinserverCmd)
}

func main() {

	if err := rootCmd.Execute(); err != nil {
		log.Printf("error executing root command: %s", err)
		os.Exit(1)
	}
}
