package client

import (
	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
)

var SignalCmd = &cli.Subcommand{
	Use:   "signal [wakeup|reset] JOB [DATA]",
	Short: "wake up a job from wait state or abort its current invocation",
	Run: func(subcommand *cli.Subcommand, args []string) error {
		return runSignalCmd(subcommand.Config(), args)
	},
}

func runSignalCmd(config *config.Config, args []string) error {
	if len(args) < 2 || len(args) > 3 {
		return errors.Errorf("Expected 2 arguments: [wakeup|reset|set-concurrency] JOB [DATA]")
	}

	var data string
	if len(args) == 3 {
		data = args[2]
	}

	httpc, err := controlHttpClient(config.Global.Control.SockPath)
	if err != nil {
		return err
	}

	err = jsonRequestResponse(httpc, daemon.ControlJobEndpointSignal,
		struct {
			Name string
			Op   string
			Data string
		}{
			Name: args[1],
			Op:   args[0],
			Data: data,
		},
		struct{}{},
	)
	return err
}
