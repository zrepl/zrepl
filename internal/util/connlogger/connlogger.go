package connlogger

import (
	"net"
	"os"
)

type NetConnLogger struct {
	net.Conn
	ReadFile  *os.File
	WriteFile *os.File
}

func NewNetConnLogger(conn net.Conn, readlog, writelog string) (l *NetConnLogger, err error) {
	l = &NetConnLogger{
		Conn: conn,
	}
	flags := os.O_CREATE | os.O_WRONLY
	if readlog != "" {
		if l.ReadFile, err = os.OpenFile(readlog, flags, 0600); err != nil {
			return
		}
	}
	if writelog != "" {
		if l.WriteFile, err = os.OpenFile(writelog, flags, 0600); err != nil {
			return
		}
	}
	return
}

func (c *NetConnLogger) Read(buf []byte) (n int, err error) {
	n, err = c.Conn.Read(buf)
	if c.WriteFile != nil {
		if _, writeErr := c.ReadFile.Write(buf[0:n]); writeErr != nil {
			panic(writeErr)
		}
	}
	return
}

func (c *NetConnLogger) Write(buf []byte) (n int, err error) {
	n, err = c.Conn.Write(buf)
	if c.ReadFile != nil {
		if _, writeErr := c.WriteFile.Write(buf[0:n]); writeErr != nil {
			panic(writeErr)
		}
	}
	return
}
func (c *NetConnLogger) Close() (err error) {
	err = c.Conn.Close()
	if err != nil {
		return
	}
	if c.ReadFile != nil {
		if err := c.ReadFile.Close(); err != nil {
			panic(err)
		}
	}
	if c.WriteFile != nil {
		if err := c.WriteFile.Close(); err != nil {
			panic(err)
		}
	}
	return
}
