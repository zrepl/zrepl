package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/zrepl/zrepl/config"
)

var rootArgs struct {
	configPath string
}

var rootCmd = &cobra.Command{
	Use:   "zrepl",
	Short: "One-stop ZFS replication solution",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rootArgs.configPath, "config", "", "config file path")
}

var genCompletionCmd = &cobra.Command{
	Use:   "gencompletion",
	Short: "generate shell auto-completions",
}

type completionCmdInfo struct {
	genFunc func(outpath string) error
	help    string
}

var completionCmdMap = map[string]completionCmdInfo{
	"zsh": {
		rootCmd.GenZshCompletionFile,
		"  save to file `_zrepl` in your zsh's $fpath",
	},
	"bash": {
		rootCmd.GenBashCompletionFile,
		"  save to a path and source that path in your .bashrc",
	},
}

func init() {
	for sh, info := range completionCmdMap {
		sh, info := sh, info
		genCompletionCmd.AddCommand(&cobra.Command{
			Use:     fmt.Sprintf("%s path/to/out/file", sh),
			Short:   fmt.Sprintf("generate %s completions", sh),
			Example: info.help,
			Run: func(cmd *cobra.Command, args []string) {
				if len(args) != 1 {
					fmt.Fprintf(os.Stderr, "specify exactly one positional agument\n")
					err := cmd.Usage()
					if err != nil {
						panic(err)
					}
					os.Exit(1)
				}
				if err := info.genFunc(args[0]); err != nil {
					fmt.Fprintf(os.Stderr, "error generating %s completion: %s", sh, err)
					os.Exit(1)
				}
			},
		})
	}
	rootCmd.AddCommand(genCompletionCmd)
}

type Subcommand struct {
	Use              string
	Short            string
	Example          string
	NoRequireConfig  bool
	Run              func(subcommand *Subcommand, args []string) error
	SetupFlags       func(f *pflag.FlagSet)
	SetupSubcommands func() []*Subcommand

	config    *config.Config
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
	addSubcommandToCobraCmd(rootCmd, s)
}

func addSubcommandToCobraCmd(c *cobra.Command, s *Subcommand) {
	cmd := cobra.Command{
		Use:     s.Use,
		Short:   s.Short,
		Example: s.Example,
	}
	if s.SetupSubcommands == nil {
		cmd.Run = s.run
	} else {
		for _, sub := range s.SetupSubcommands() {
			addSubcommandToCobraCmd(&cmd, sub)
		}
	}
	if s.SetupFlags != nil {
		s.SetupFlags(cmd.Flags())
	}
	c.AddCommand(&cmd)
}

func Run() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
