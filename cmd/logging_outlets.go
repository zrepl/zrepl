package cmd

import (
	"context"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/logger"
	"io"
	"net"
	"time"
)

type WriterOutlet struct {
	Formatter EntryFormatter
	Writer    io.Writer
}

func (h WriterOutlet) WriteEntry(ctx context.Context, entry logger.Entry) error {
	bytes, err := h.Formatter.Format(&entry)
	if err != nil {
		return err
	}
	_, err = h.Writer.Write(bytes)
	h.Writer.Write([]byte("\n"))
	return err
}

type TCPOutlet struct {
	Formatter     EntryFormatter
	Net, Address  string
	Dialer        net.Dialer
	RetryInterval time.Duration
	conn          net.Conn
	retry         time.Time
}

func (h *TCPOutlet) WriteEntry(ctx context.Context, e logger.Entry) error {

	b, err := h.Formatter.Format(&e)
	if err != nil {
		return err
	}

	if h.conn == nil {
		if time.Now().Sub(h.retry) < h.RetryInterval {
			return nil // this is not an error toward the logger
			//return errors.New("TCP hook reconnect prohibited by retry interval")
		}
		h.conn, err = h.Dialer.DialContext(ctx, h.Net, h.Address)
		if err != nil {
			h.retry = time.Now()
			return errors.Wrap(err, "cannot dial")
		}
	}

	_, err = h.conn.Write(b)
	if err != nil {
		h.conn.Close()
		h.conn = nil
		return err
	}

	return nil
}
