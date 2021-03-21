package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"github.com/zrepl/zrepl/daemon/job"
)

var SignalCmd = &cli.Subcommand{
	Use:   "signal JOB [replication|reset|snapshot]",
	Short: "run a job replication, abort its current invocation, run a snapshot job",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return runSignalCmd(subcommand.Config(), args)
	},
}

func runSignalCmd(config *config.Config, args []string) error {
	if len(args) != 2 {
		return errors.Errorf("Expected 2 arguments: [replication|reset|snapshot] JOB")
	}

	httpc, err := controlHttpClient(config.Global.Control.SockPath)
	if err != nil {
		return err
	}

	jobName := args[0]
	what := args[1]

	var res job.ActiveSideSignalResponse
	err = jsonRequestResponse(httpc, daemon.ControlJobEndpointSignalActive,
		struct {
			Job string
			job.ActiveSideSignalRequest
		}{
			Job: jobName,
			ActiveSideSignalRequest: job.ActiveSideSignalRequest{
				What: what,
			},
		},
		&res,
	)

	pollRequest := daemon.ControlJobEndpointSignalActiveRequest{
		Job: jobName,
		ActiveSidePollRequest: job.ActiveSidePollRequest{
			InvocationId: res.InvocationId,
			What:         what,
		},
	}

	j, err := json.Marshal(pollRequest)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(j))
	return err
}
