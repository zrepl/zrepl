package sshdirect

import (
	"bytes"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/hashicorp/yamux"
)

type ServeConn struct {
	stdin, stdout *os.File
}

var _ net.Conn = (*ServeConn)(nil)

func ServeStdin() (net.Listener, error) {

	conn := &ServeConn{
		stdin:  os.Stdin,
		stdout: os.Stdout,
	}

	var buf bytes.Buffer
	buf.Write(banner_msg)
	if _, err := io.Copy(conn, &buf); err != nil {
		log.Printf("error sending confirm message: %s", err)
		conn.Close()
		return nil, err
	}
	buf.Reset()
	if _, err := io.CopyN(&buf, conn, int64(len(begin_msg))); err != nil {
		log.Printf("error reading begin message: %s", err)
		conn.Close()
		return nil, err
	}

	return yamux.Server(conn, nil)
}

func (c *ServeConn) Read(p []byte) (int, error) {
	return c.stdin.Read(p)
}

func (c *ServeConn) Write(p []byte) (int, error) {
	return c.stdout.Write(p)
}

func (f *ServeConn) Close() (err error) {
	e1 := f.stdin.Close()
	e2 := f.stdout.Close()
	// FIXME merge errors
	if e1 != nil {
		return e1
	}
	return e2
}

func (f *ServeConn) SetReadDeadline(t time.Time) error {
	return f.stdin.SetReadDeadline(t)
}

func (f *ServeConn) SetWriteDeadline(t time.Time) error {
	return f.stdout.SetReadDeadline(t)
}

func (f *ServeConn) SetDeadline(t time.Time) error {
	// try both...
	werr := f.SetWriteDeadline(t)
	rerr := f.SetReadDeadline(t)
	if werr != nil {
		return werr
	}
	if rerr != nil {
		return rerr
	}
	return nil
}

type serveAddr struct{}

const GoNetwork string = "sshdirect"

func (serveAddr) Network() string { return GoNetwork }
func (serveAddr) String() string  { return "???" }

func (f *ServeConn) LocalAddr() net.Addr  { return serveAddr{} }
func (f *ServeConn) RemoteAddr() net.Addr { return serveAddr{} }
