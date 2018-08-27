package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "print version of zrepl binary (for running daemon 'zrepl control version' command)",
	Run:   doVersion,
}

func init() {
	RootCmd.AddCommand(versionCmd)
}

func doVersion(cmd *cobra.Command, args []string) {
	fmt.Println(version.NewZreplVersionInformation().String())
}
