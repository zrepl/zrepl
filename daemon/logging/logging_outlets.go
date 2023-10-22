package logging

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/syslog"
	"net"
	"os"
	"syscall"
	"text/template"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/logger"
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
	if err != nil {
		return err
	}
	_, err = h.writer.Write([]byte("\n"))
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

	entryChan := make(chan *bytes.Buffer, 1) // allow one message in flight while previous is in io.Copy()

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
		err = conn.SetWriteDeadline(time.Now().Add(retryInterval))
		if err == nil {
			_, err = io.Copy(conn, msg)
		}
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
		o.writer, err = syslog.New(o.Facility, "zrepl")
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

type FileOutlet struct {
	file      *os.File
	filename  string
	formatter EntryFormatter
	template  *template.Template
	writer    io.Writer
}

func (self *FileOutlet) WriteEntry(entry logger.Entry) error {
	bytes, err := self.formatter.Format(&entry)
	if err != nil {
		return err
	}

	if err := self.reOpenIfNotExists(); err != nil {
		return nil
	}

	if self.template == nil {
		return self.writeBytes(bytes)
	}

	if err := self.writeTemplate(entry.Time, string(bytes)); err != nil {
		return err
	}

	return nil
}

func (self *FileOutlet) reOpenIfNotExists() error {
	finfo, err := self.file.Stat()
	if err != nil {
		return fmt.Errorf("failed stat of %q: %w", self.filename, err)
	}

	nlink := uint64(0)
	if finfo.Sys() != nil {
		if stat, ok := finfo.Sys().(*syscall.Stat_t); ok {
			nlink = stat.Nlink
		}
	}
	if nlink > 0 {
		return nil
	}

	return self.reOpen()
}

func (self *FileOutlet) reOpen() error {
	if err := self.file.Close(); err != nil {
		return fmt.Errorf("failed close %q: %w", self.filename, err)
	}

	return self.Open()
}

func (self *FileOutlet) Open() error {
	f, err := os.OpenFile(self.filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("file outlet: %w", err)
	}
	self.file = f
	self.writer = f

	return nil
}

func (self *FileOutlet) ParseTemplate(templateText string) error {
	funcMap := template.FuncMap{
		"formatTime": func(t time.Time, layout string) string {
			return t.Format(layout)
		},
	}

	tmpl, err := template.New("").Funcs(funcMap).Parse(templateText)
	if err != nil {
		return fmt.Errorf("failed parse template %q: %w", templateText, err)
	}
	self.template = tmpl

	return nil
}

func (self *FileOutlet) writeTemplate(t time.Time, msg string) error {
	data := struct {
		Time    time.Time
		Pid     int
		Message string
	}{
		Time:    t,
		Pid:     os.Getpid(),
		Message: msg,
	}

	if err := self.template.Execute(self.writer, data); err != nil {
		return fmt.Errorf("failed execute template: %w", err)
	}

	return nil
}

func (self *FileOutlet) writeBytes(bytes []byte) error {
	if _, err := self.writer.Write(bytes); err != nil {
		return fmt.Errorf("failed write to %q: %w", self.filename, err)
	}

	if _, err := self.writer.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed write to %q: %w", self.filename, err)
	}

	return nil
}
