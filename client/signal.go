package client

import (
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
)

func RunSignal(config *config.Config, args []string) error {
	if len(args) != 2 {
		return errors.Errorf("Expected 2 arguments: [wakeup|reset] JOB")
	}

	httpc, err := controlHttpClient(config.Global.Control.SockPath)
	if err != nil {
		return err
	}

	err = jsonRequestResponse(httpc, daemon.ControlJobEndpointSignal,
		struct {
			Name string
			Op string
		}{
			Name: args[1],
			Op: args[0],
		},
		struct{}{},
	)
	return err
}
