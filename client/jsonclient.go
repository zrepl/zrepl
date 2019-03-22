package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"

	"github.com/pkg/errors"
)

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
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
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
