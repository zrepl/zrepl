package client

import (
	"encoding/json"
	"github.com/kr/pretty"
	"github.com/spf13/pflag"
	"github.com/zrepl/yaml-config"
	"github.com/zrepl/zrepl/cli"
	"os"
)

var configcheckArgs struct {
	format string
}

var ConfigcheckCmd = &cli.Subcommand{
	Use: "configcheck",
	Short: "check if config can be parsed without errors",
	SetupFlags: func(f *pflag.FlagSet) {
		f.StringVar(&configcheckArgs.format, "format", "", "dump parsed config object [pretty|yaml|json]")
	},
	Run: func(subcommand *cli.Subcommand, args []string) error {
		switch configcheckArgs.format {
		case "pretty":
			_, err := pretty.Println(subcommand.Config())
			return err
		case "json":
			return json.NewEncoder(os.Stdout).Encode(subcommand.Config())
		case "yaml":
			return yaml.NewEncoder(os.Stdout).Encode(subcommand.Config())
		default: // no output
		}
		return nil
	},
}

