package status

import (
	"bytes"
	"io"
	"os"
)

func raw(c Client) error {
	b, err := c.StatusRaw()
	if err != nil {
		return err
	}
	if _, err := io.Copy(os.Stdout, bytes.NewReader(b)); err != nil {
		return err
	}
	return nil
}
