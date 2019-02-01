package logging

import (
	"bytes"
	"context"
	"crypto/tls"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/logger"
	"io"
	"log/syslog"
	"net"
	"time"
)

type EntryFormatter interface {
	SetMetadataFlags(flags MetadataFlags)
	Format(e *logger.Entry) ([]byte, error)
}

type WriterOutlet struct {
	formatter EntryFormatter
	writer    io.Writer
}

func (h WriterOutlet) WriteEntry(entry logger.Entry) error {
	bytes, err := h.formatter.Format(&entry)
	if err != nil {
		return err
	}
	_, err = h.writer.Write(bytes)
	h.writer.Write([]byte("\n"))
	return err
}

type TCPOutlet struct {
	formatter EntryFormatter
	// Specifies how much time must pass between a connection error and a reconnection attempt
	// Log entries written to the outlet during this time interval are silently dropped.
	connect   func(ctx context.Context) (net.Conn, error)
	entryChan chan *bytes.Buffer
}

func NewTCPOutlet(formatter EntryFormatter, network, address string, tlsConfig *tls.Config, retryInterval time.Duration) *TCPOutlet {

	connect := func(ctx context.Context) (conn net.Conn, err error) {
		deadl, ok := ctx.Deadline()
		if !ok {
			deadl = time.Time{}
		}
		dialer := net.Dialer{
			Deadline: deadl,
		}
		if tlsConfig != nil {
			conn, err = tls.DialWithDialer(&dialer, network, address, tlsConfig)
		} else {
			conn, err = dialer.DialContext(ctx, network, address)
		}
		return
	}

	entryChan := make(chan *bytes.Buffer, 1) // allow one message in flight while previos is in io.Copy()

	o := &TCPOutlet{
		formatter: formatter,
		connect:   connect,
		entryChan: entryChan,
	}

	go o.outLoop(retryInterval)

	return o
}

// FIXME: use this method
func (h *TCPOutlet) Close() {
	close(h.entryChan)
}

func (h *TCPOutlet) outLoop(retryInterval time.Duration) {

	var retry time.Time
	var conn net.Conn
	for msg := range h.entryChan {
		var err error
		for conn == nil {
			time.Sleep(time.Until(retry))
			ctx, cancel := context.WithDeadline(context.TODO(), time.Now().Add(retryInterval))
			conn, err = h.connect(ctx)
			cancel()
			if err != nil {
				retry = time.Now().Add(retryInterval)
				conn = nil
			}
		}
		conn.SetWriteDeadline(time.Now().Add(retryInterval))
		_, err = io.Copy(conn, msg)
		if err != nil {
			retry = time.Now().Add(retryInterval)
			conn.Close()
			conn = nil
		}
	}
}

func (h *TCPOutlet) WriteEntry(e logger.Entry) error {

	ebytes, err := h.formatter.Format(&e)
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	buf.Write(ebytes)
	buf.WriteString("\n")

	select {
	case h.entryChan <- buf:
		return nil
	default:
		return errors.New("connection broken or not fast enough")
	}
}

type SyslogOutlet struct {
	Formatter          EntryFormatter
	RetryInterval      time.Duration
	Facility           syslog.Priority
	writer             *syslog.Writer
	lastConnectAttempt time.Time
}

func (o *SyslogOutlet) WriteEntry(entry logger.Entry) error {

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
		fac := syslog.LOG_LOCAL0
		switch o.Facility {
			case "kern":     fac = syslog.LOG_KERN
			case "user":     fac = syslog.LOG_USER
			case "mail":     fac = syslog.LOG_MAIL
			case "daemon":   fac = syslog.LOG_DAEMON
			case "auth":     fac = syslog.LOG_AUTH
			case "syslog":   fac = syslog.LOG_SYSLOG
			case "lpr":      fac = syslog.LOG_LPR
			case "news":     fac = syslog.LOG_NEWS
			case "uucp":     fac = syslog.LOG_UUCP
			case "cron":     fac = syslog.LOG_CRON
			case "authpriv": fac = syslog.LOG_AUTHPRIV
			case "ftp":      fac = syslog.LOG_FTP
			case "local1":   fac = syslog.LOG_LOCAL1
			case "local2":   fac = syslog.LOG_LOCAL2
			case "local3":   fac = syslog.LOG_LOCAL3
			case "local4":   fac = syslog.LOG_LOCAL4
			case "local5":   fac = syslog.LOG_LOCAL5
			case "local6":   fac = syslog.LOG_LOCAL6
			case "local7":   fac = syslog.LOG_LOCAL7
		}
		o.writer, err = syslog.New(fac, "zrepl")
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
