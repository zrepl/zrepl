package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"os"
)

var bashcompCmd = &cobra.Command{
	Use:   "bashcomp path/to/out/file",
	Short: "generate bash completions",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			fmt.Fprintf(os.Stderr, "specify exactly one positional agument\n")
			cmd.Usage()
			os.Exit(1)
		}
		if err := RootCmd.GenBashCompletionFile(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "error generating bash completion: %s", err)
			os.Exit(1)
		}
	},
	Hidden: true,
}

func init() {
	RootCmd.AddCommand(bashcompCmd)
}
