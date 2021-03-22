package client

import (
	"context"
	"strconv"
	"time"

	"github.com/kr/pretty"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"github.com/zrepl/zrepl/daemon/job"
)

var resetCmdArgs struct {
	verbose  bool
	interval time.Duration
	token    string
}

var ResetCmd = &cli.Subcommand{
	Use:   "reset [-t TOKEN | JOB INVOCATION [replication|snapshotting|prune_sender|prune_receiver]]",
	Short: "",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return runResetCmd(subcommand.Config(), args)
	},
	SetupFlags: func(f *pflag.FlagSet) {
		f.BoolVarP(&resetCmdArgs.verbose, "verbose", "v", false, "verbose output")
		f.DurationVarP(&resetCmdArgs.interval, "poll-interval", "i", 100*time.Millisecond, "poll interval")
		f.StringVarP(&resetCmdArgs.token, "token", "t", "", "token produced by 'signal' subcommand")
	},
}

func runResetCmd(config *config.Config, args []string) error {

	httpc, err := controlHttpClient(config.Global.Control.SockPath)
	if err != nil {
		return err
	}

	var req daemon.ControlJobEndpointResetActiveRequest
	if resetCmdArgs.token != "" {
		var token TriggerToken
		err := token.Decode(resetCmdArgs.token)
		if err != nil {
			return errors.Wrap(err, "cannot decode token")
		}
		req = token.ToReset()
	} else {
		jobName := args[0]

		invocationId, err := strconv.ParseUint(args[1], 10, 64)
		if err != nil {
			return errors.Wrap(err, "parse invocation id")
		}

		// what := args[2]

		// updated by subsequent requests
		req = daemon.ControlJobEndpointResetActiveRequest{
			Job: jobName,
			ActiveSideResetRequest: job.ActiveSideResetRequest{
				InvocationId: invocationId,
			},
		}
	}

	var res job.ActiveSideResetResponse
	if resetCmdArgs.verbose {
		pretty.Println("making request", req)
	}
	err = jsonRequestResponse(httpc, daemon.ControlJobEndpointResetActive,
		req,
		&res,
	)
	if err != nil {
		return err
	}

	if resetCmdArgs.verbose {
		pretty.Println("got response", res)
	}

	return nil
}
