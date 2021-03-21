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

var TriggerCmd = &cli.Subcommand{
	Use:   "trigger JOB [replication|snapshot]",
	Short: "",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return runTriggerCmd(subcommand.Config(), args)
	},
}

type TriggerToken struct {
	// TODO version, daemon invocation id, etc.
	Job          string
	InvocationId uint64
}

func (t TriggerToken) Encode() string {
	j, err := json.Marshal(t)
	if err != nil {
		panic(err)
	}
	return string(j)
}

func (t *TriggerToken) Decode(s string) error {
	return json.Unmarshal([]byte(s), t)
}

func (t TriggerToken) ToReset() daemon.ControlJobEndpointResetActiveRequest {
	return daemon.ControlJobEndpointResetActiveRequest{
		Job: t.Job,
		ActiveSideResetRequest: job.ActiveSideResetRequest{
			InvocationId: t.InvocationId,
		},
	}
}

func (t TriggerToken) ToWait() daemon.ControlJobEndpointWaitActiveRequest {
	return daemon.ControlJobEndpointWaitActiveRequest{
		Job: t.Job,
		ActiveSidePollRequest: job.ActiveSidePollRequest{
			InvocationId: t.InvocationId,
		},
	}
}

func runTriggerCmd(config *config.Config, args []string) error {
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
	err = jsonRequestResponse(httpc, daemon.ControlJobEndpointTriggerActive,
		struct {
			Job string
			job.ActiveSideTriggerRequest
		}{
			Job: jobName,
			ActiveSideTriggerRequest: job.ActiveSideTriggerRequest{
				What: what,
			},
		},
		&res,
	)

	token := TriggerToken{
		Job:          jobName,
		InvocationId: res.InvocationId,
	}

	fmt.Println(token.Encode())
	return err
}
