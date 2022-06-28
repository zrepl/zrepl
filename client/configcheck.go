package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/kr/pretty"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"github.com/zrepl/yaml-config"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/job"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/logger"
)

var parseFlags config.ParseFlags
var skipCertCheck bool

var configcheckArgs struct {
	format string
	what   string
}

var ConfigcheckCmd = &cli.Subcommand{
	Use:   "configcheck",
	Short: "check if config can be parsed without errors",
	SetupFlags: func(f *pflag.FlagSet) {
		f.StringVar(&configcheckArgs.format, "format", "", "dump parsed config object [pretty|yaml|json]")
		f.StringVar(&configcheckArgs.what, "what", "all", "what to print [all|config|jobs|logging]")

		// FIXME: can't set parseFlags here because user input hasn't been populated yet
		f.BoolVar(&skipCertCheck, "skip-cert-check", false, "skip checking cert files")
	},
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		formatMap := map[string]func(interface{}){
			"": func(i interface{}) {},
			"pretty": func(i interface{}) {
				if _, err := pretty.Println(i); err != nil {
					panic(err)
				}
			},
			"json": func(i interface{}) {
				if err := json.NewEncoder(os.Stdout).Encode(subcommand.Config()); err != nil {
					panic(err)
				}
			},
			"yaml": func(i interface{}) {
				if err := yaml.NewEncoder(os.Stdout).Encode(subcommand.Config()); err != nil {
					panic(err)
				}
			},
		}

		formatter, ok := formatMap[configcheckArgs.format]
		if !ok {
			return fmt.Errorf("unsupported --format %q", configcheckArgs.format)
		}

		var hadErr bool

		if skipCertCheck {
			parseFlags |= config.ParseFlagsNoCertCheck
		}

		// further: try to build jobs
		confJobs, err := job.JobsFromConfig(subcommand.Config(), parseFlags)

		if err != nil {
			err := errors.Wrap(err, "cannot build jobs from config")
			if configcheckArgs.what == "jobs" {
				return err
			} else {
				fmt.Fprintf(os.Stderr, "%s\n", err)
				confJobs = nil
				hadErr = true
			}
		}

		// further: try to build logging outlets
		outlets, err := logging.OutletsFromConfig(*subcommand.Config().Global.Logging)
		if err != nil {
			err := errors.Wrap(err, "cannot build logging from config")
			if configcheckArgs.what == "logging" {
				return err
			} else {
				fmt.Fprintf(os.Stderr, "%s\n", err)
				outlets = nil
				hadErr = true
			}
		}

		whatMap := map[string]func(){
			"all": func() {
				o := struct {
					config  *config.Config
					jobs    []job.Job
					logging *logger.Outlets
				}{
					subcommand.Config(),
					confJobs,
					outlets,
				}
				formatter(o)
			},
			"config": func() {
				formatter(subcommand.Config())
			},
			"jobs": func() {
				formatter(confJobs)
			},
			"logging": func() {
				formatter(outlets)
			},
		}

		wf, ok := whatMap[configcheckArgs.what]
		if !ok {
			return fmt.Errorf("unsupported --format %q", configcheckArgs.what)
		}
		wf()

		if hadErr {
			return fmt.Errorf("config parsing failed")
		} else {
			return nil
		}
	},
}
