package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/internal/daemon"
)

type Client struct {
	h *http.Client
}

func New(network, addr string) (*Client, error) {
	httpc, err := makeControlHttpClient(func(_ context.Context) (net.Conn, error) { return net.Dial(network, addr) })
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

func (c *Client) SignalReset(job string) error {
	return c.signal(job, "reset")
}

func makeControlHttpClient(dialfunc func(context.Context) (net.Conn, error)) (client *http.Client, err error) {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialfunc(ctx)
			},
		},
	}, nil
}

func jsonRequestResponse(c *http.Client, endpoint string, req interface{}, res interface{}) error {
	var buf bytes.Buffer
	encodeErr := json.NewEncoder(&buf).Encode(req)
	if encodeErr != nil {
		return encodeErr
	}

	hreq, err := http.NewRequest("POST", "http://unix"+endpoint, &buf)
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	// Prevent EOF errors when client request frequency and server keepalive close are at the same time.
	// Found this by watching http.Server.ConnState changes, then found
	// https://stackoverflow.com/questions/17714494/golang-http-request-results-in-eof-errors-when-making-multiple-requests-successi
	// Note: The issue seems even more prounounced with local TCP sockets than unix domain sockets. So, I used that for debugging.
	hreq.Close = true
	resp, err := c.Do(hreq)
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
