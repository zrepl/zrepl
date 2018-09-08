// See cmd package.
package main

import (
	"errors"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/client"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"log"
	"os"
	"fmt"
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


var bashcompCmd = &cobra.Command{
	Use:   "bashcomp path/to/out/file",
	Short: "generate bash completions",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			fmt.Fprintf(os.Stderr, "specify exactly one positional agument\n")
			cmd.Usage()
			os.Exit(1)
		}
		if err := rootCmd.GenBashCompletionFile(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "error generating bash completion: %s", err)
			os.Exit(1)
		}
	},
	Hidden: true,
}

var configcheckCmd = &cobra.Command{
	Use: "configcheck",
	Short: "validate config file",
	RunE: func(cmd *cobra.Command, args []string) error {
		conf, err := config.ParseConfig(rootArgs.configFile)
		if err != nil {
			return err
		}
		return client.RunConfigcheck(conf, args)
	},
}

var versionCmdArgs client.VersionArgs
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "print version of zrepl binary (for running daemon 'zrepl control version' command)",
	Run: func(cmd *cobra.Command, args []string) {
		conf, err := config.ParseConfig(rootArgs.configFile)
		if err == nil {
			versionCmdArgs.Config = conf
		}
		client.RunVersion(versionCmdArgs)
	},
}

var pprofCmd = &cobra.Command{
	Use:   "pprof off | [on TCP_LISTEN_ADDRESS]",
	Short: "start a http server exposing go-tool-compatible profiling endpoints at TCP_LISTEN_ADDRESS",
	RunE: func(cmd *cobra.Command, args []string) error {
		conf, err := config.ParseConfig(rootArgs.configFile)
		if err != nil {
			return err
		}

		var pprofCmdArgs client.PProfArgs
		if cmd.Flags().NArg() < 1 {
			goto enargs
		}
		switch cmd.Flags().Arg(0) {
		case "on":
			pprofCmdArgs.Run = true
			if cmd.Flags().NArg() != 2 {
				return errors.New("must specify TCP_LISTEN_ADDRESS as second positional argument")
			}
			pprofCmdArgs.HttpListenAddress = cmd.Flags().Arg(1)
		case "off":
			if cmd.Flags().NArg() != 1 {
				goto enargs
			}
			pprofCmdArgs.Run = false
		}

		client.RunPProf(conf, pprofCmdArgs)
		return nil
	enargs:
		return errors.New("invalid number of positional arguments")

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
	rootCmd.AddCommand(bashcompCmd)
	rootCmd.AddCommand(configcheckCmd)
	versionCmd.Flags().StringVar(&versionCmdArgs.Show, "show", "", "version info to show (client|daemon)")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(pprofCmd)
}

func main() {

	if err := rootCmd.Execute(); err != nil {
		log.Printf("error executing root command: %s", err)
		os.Exit(1)
	}
}
