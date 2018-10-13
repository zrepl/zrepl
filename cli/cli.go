package cli

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/zrepl/zrepl/config"
	"os"
)

var rootArgs struct {
	configPath string
}

var rootCmd = &cobra.Command{
	Use:   "zrepl",
	Short: "One-stop ZFS replication solution",
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

func init() {
	rootCmd.PersistentFlags().StringVar(&rootArgs.configPath, "config", "", "config file path")
	rootCmd.AddCommand(bashcompCmd)
}

type Subcommand struct {
	Use             string
	Short           string
	NoRequireConfig bool
	Run             func(subcommand *Subcommand, args []string) error
	SetupFlags      func(f *pflag.FlagSet)

	config *config.Config
	configErr error
}

func (s *Subcommand) ConfigParsingError() error {
	return s.configErr
}

func (s *Subcommand) Config() *config.Config {
	if !s.NoRequireConfig && s.config == nil {
		panic("command that requires config is running and has no config set")
	}
	return s.config
}

func (s *Subcommand) run(cmd *cobra.Command, args []string) {
	s.tryParseConfig()
	err := s.Run(s, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func (s *Subcommand) tryParseConfig() {
	config, err := config.ParseConfig(rootArgs.configPath)
	s.configErr = err
	if err != nil {
		if s.NoRequireConfig {
			// doesn't matter
			return
		} else {
			fmt.Fprintf(os.Stderr, "could not parse config: %s\n", err)
			os.Exit(1)
		}
	}
	s.config = config
}

func AddSubcommand(s *Subcommand) {
	cmd := cobra.Command{
		Use: s.Use,
		Short: s.Short,
		Run: s.run,
	}
	if s.SetupFlags != nil {
		s.SetupFlags(cmd.Flags())
	}
	rootCmd.AddCommand(&cmd)
}


func Run() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}