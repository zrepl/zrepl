package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"runtime"
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
	fmt.Println(NewZreplVersionInformation().String())
}

type ZreplVersionInformation struct {
	Version         string
	RuntimeGOOS     string
	RuntimeGOARCH   string
	RUNTIMECompiler string
}

func NewZreplVersionInformation() *ZreplVersionInformation {
	return &ZreplVersionInformation{
		Version:         zreplVersion,
		RuntimeGOOS:     runtime.GOOS,
		RuntimeGOARCH:   runtime.GOARCH,
		RUNTIMECompiler: runtime.Compiler,
	}
}

func (i *ZreplVersionInformation) String() string {
	return fmt.Sprintf("zrepl version=%s GOOS=%s GOARCH=%s Compiler=%s",
		i.Version, i.RuntimeGOOS, i.RuntimeGOARCH, i.RUNTIMECompiler)
}
