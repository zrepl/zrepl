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
	verbose bool
	interval time.Duration
}

var WaitCmd = &cli.Subcommand{
	Use:   "wait [active JOB INVOCATION_ID WHAT]",
	Short: "",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return runWaitCmd(subcommand.Config(), args)
	},
	SetupFlags: func(f *pflag.FlagSet) {
		f.BoolVarP(&waitCmdArgs.verbose, "verbose", "v", false, "verbose output")
		f.DurationVarP(&waitCmdArgs.interval, "poll-interval", "i", 100*time.Millisecond, "poll interval")
	},
}

func runWaitCmd(config *config.Config, args []string) error {

	httpc, err := controlHttpClient(config.Global.Control.SockPath)
	if err != nil {
		return err
	}

	if args[0] != "active" {
		panic(args)
	}
	args = args[1:]

	jobName := args[0]

	invocationId, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return errors.Wrap(err, "parse invocation id")
	}

	waitWhat := args[2]

	doneErr := fmt.Errorf("done")

	var pollRequest job.ActiveSidePollRequest

	// updated by subsequent requests
	pollRequest = job.ActiveSidePollRequest{
		InvocationId: invocationId,
		What:         waitWhat,
	}

	pollOnce := func() error {
		var res job.ActiveSidePollResponse
		if waitCmdArgs.verbose {
			pretty.Println("making poll request", pollRequest)
		}
		err = jsonRequestResponse(httpc, daemon.ControlJobEndpointPollActive,
			struct {
				Job string
				job.ActiveSidePollRequest
			}{
				Job:                   jobName,
				ActiveSidePollRequest: pollRequest,
			},
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

		pollRequest.InvocationId = res.InvocationId

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
