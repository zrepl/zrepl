package client

import (
	"context"
	"fmt"
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

var waitCmdArgs struct {
	verbose  bool
	interval time.Duration
	token    string
}

var WaitCmd = &cli.Subcommand{
	Use:   "wait [-t TOKEN | JOB INVOCATION [replication|snapshotting|prune_sender|prune_receiver]]",
	Short: "",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return runWaitCmd(subcommand.Config(), args)
	},
	SetupFlags: func(f *pflag.FlagSet) {
		f.BoolVarP(&waitCmdArgs.verbose, "verbose", "v", false, "verbose output")
		f.DurationVarP(&waitCmdArgs.interval, "poll-interval", "i", 100*time.Millisecond, "poll interval")
		f.StringVarP(&waitCmdArgs.token, "token", "t", "", "token produced by 'signal' subcommand")
	},
}

func runWaitCmd(config *config.Config, args []string) error {

	httpc, err := controlHttpClient(config.Global.Control.SockPath)
	if err != nil {
		return err
	}

	var req daemon.ControlJobEndpointWaitActiveRequest
	if waitCmdArgs.token != "" {
		var token TriggerToken
		err := token.Decode(resetCmdArgs.token)
		if err != nil {
			return errors.Wrap(err, "cannot decode token")
		}
		req = token.ToWait()
	} else {

		jobName := args[0]

		invocationId, err := strconv.ParseUint(args[1], 10, 64)
		if err != nil {
			return errors.Wrap(err, "parse invocation id")
		}

		// updated by subsequent requests
		req = daemon.ControlJobEndpointWaitActiveRequest{
			Job: jobName,
			ActiveSidePollRequest: job.ActiveSidePollRequest{
				InvocationId: invocationId,
			},
		}
	}

	doneErr := fmt.Errorf("done")

	pollOnce := func() error {
		var res job.ActiveSidePollResponse
		if waitCmdArgs.verbose {
			pretty.Println("making poll request", req)
		}
		err = jsonRequestResponse(httpc, daemon.ControlJobEndpointPollActive,
			req,
			&res,
		)
		if err != nil {
			return err
		}

		if waitCmdArgs.verbose {
			pretty.Println("got poll response", res)
		}

		if res.Done {
			return doneErr
		}

		req.InvocationId = res.InvocationId

		return nil
	}

	t := time.NewTicker(waitCmdArgs.interval)
	for range t.C {
		err := pollOnce()
		if err == doneErr {
			return nil
		} else if err != nil {
			return err
		}
	}

	return err
}
