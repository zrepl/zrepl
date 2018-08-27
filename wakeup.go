package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"io"
	"net"
	"net/http"
)

func RunWakeup(config config.Config, args []string) error {
	if len(args) != 1 {
		return errors.Errorf("Expected 1 argument: job")
	}

	httpc, err := controlHttpClient(config.Global.Control.SockPath)
	if err != nil {
		return err
	}

	err = jsonRequestResponse(httpc, daemon.ControlJobEndpointWakeup,
		struct {
			Name string
		}{
			Name: args[0],
		},
		struct{}{},
	)
	return err
}

func controlHttpClient(sockpath string) (client http.Client, err error) {
	return http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sockpath)
			},
		},
	}, nil
}

func jsonRequestResponse(c http.Client, endpoint string, req interface{}, res interface{}) error {
	var buf bytes.Buffer
	encodeErr := json.NewEncoder(&buf).Encode(req)
	if encodeErr != nil {
		return encodeErr
	}

	resp, err := c.Post("http://unix"+endpoint, "application/json", &buf)
	if err != nil {
		return err
	} else if resp.StatusCode != http.StatusOK {
		var msg bytes.Buffer
		io.CopyN(&msg, resp.Body, 4096)
		return errors.Errorf("%s", msg.String())
	}

	decodeError := json.NewDecoder(resp.Body).Decode(&res)
	if decodeError != nil {
		return decodeError
	}

	return nil
}
