package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/daemon"
)

type Client struct {
	h http.Client
}

func New(network, addr string) (*Client, error) {
	httpc, err := controlHttpClient(func(_ context.Context) (net.Conn, error) { return net.Dial(network, addr) })
	if err != nil {
		return nil, err
	}
	return &Client{httpc}, nil
}

func (c *Client) Status() (s daemon.Status, _ error) {
	err := jsonRequestResponse(c.h, daemon.ControlJobEndpointStatus,
		struct{}{},
		&s,
	)
	return s, err
}

func (c *Client) StatusRaw() ([]byte, error) {
	var r json.RawMessage
	err := jsonRequestResponse(c.h, daemon.ControlJobEndpointStatus, struct{}{}, &r)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (c *Client) signal(job, sig string) error {
	return jsonRequestResponse(c.h, daemon.ControlJobEndpointSignal,
		struct {
			Name string
			Op   string
		}{
			Name: job,
			Op:   sig,
		},
		struct{}{},
	)
}

func (c *Client) SignalWakeup(job string) error {
	return c.signal(job, "wakeup")
}

func (c *Client) SignalSnapshot(job string) error {
	return c.signal(job, "snapshot")
}

func (c *Client) SignalPrune(job string) error {
	return c.signal(job, "prune")
}

func (c *Client) SignalReset(job string) error {
	return c.signal(job, "reset")
}

func controlHttpClient(dialfunc func(context.Context) (net.Conn, error)) (client http.Client, err error) {
	return http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialfunc(ctx)
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
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var msg bytes.Buffer
		_, _ = io.CopyN(&msg, resp.Body, 4096) // ignore error, just display what we got
		return errors.Errorf("%s", msg.String())
	}

	decodeError := json.NewDecoder(resp.Body).Decode(&res)
	if decodeError != nil {
		return decodeError
	}

	return nil
}
