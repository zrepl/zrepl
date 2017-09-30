package cmd

import (
	"context"
	"crypto/tls"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/logger"
	"io"
	"log/syslog"
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
	Formatter    EntryFormatter
	Net, Address string
	Dialer       net.Dialer
	TLS          *tls.Config
	// Specifies how much time must pass between a connection error and a reconnection attempt
	// Log entries written to the outlet during this time interval are silently dropped.
	RetryInterval time.Duration
	// nil if there was an error sending / connecting to remote server
	conn net.Conn
	// Last time an error occurred when sending / connecting to remote server
	retry time.Time
}

func (h *TCPOutlet) WriteEntry(ctx context.Context, e logger.Entry) error {

	b, err := h.Formatter.Format(&e)
	if err != nil {
		return err
	}

	if h.conn == nil {
		if time.Now().Sub(h.retry) < h.RetryInterval {
			// cool-down phase, drop the log entry
			return nil
		}

		if h.TLS != nil {
			h.conn, err = tls.DialWithDialer(&h.Dialer, h.Net, h.Address, h.TLS)
		} else {
			h.conn, err = h.Dialer.DialContext(ctx, h.Net, h.Address)
		}
		if err != nil {
			h.conn = nil
			h.retry = time.Now()
			return errors.Wrap(err, "cannot dial")
		}
	}

	_, err = h.conn.Write(b)
	if err == nil {
		_, err = h.conn.Write([]byte("\n"))
	}
	if err != nil {
		h.conn.Close()
		h.conn = nil
		h.retry = time.Now()
		return errors.Wrap(err, "cannot write")
	}

	return nil
}

type SyslogOutlet struct {
	Formatter          EntryFormatter
	RetryInterval      time.Duration
	writer             *syslog.Writer
	lastConnectAttempt time.Time
}

func (o *SyslogOutlet) WriteEntry(ctx context.Context, entry logger.Entry) error {

	bytes, err := o.Formatter.Format(&entry)
	if err != nil {
		return err
	}

	s := string(bytes)

	if o.writer == nil {
		now := time.Now()
		if now.Sub(o.lastConnectAttempt) < o.RetryInterval {
			return nil // not an error toward logger
		}
		o.writer, err = syslog.New(syslog.LOG_LOCAL0, "zrepl")
		o.lastConnectAttempt = time.Now()
		if err != nil {
			o.writer = nil
			return err
		}
	}

	switch entry.Level {
	case logger.Debug:
		return o.writer.Debug(s)
	case logger.Info:
		return o.writer.Info(s)
	case logger.Warn:
		return o.writer.Warning(s)
	case logger.Error:
		return o.writer.Err(s)
	default:
		return o.writer.Err(s) // write as error as reaching this case is in fact an error
	}

}
