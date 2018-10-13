package socketpair

import (
	"golang.org/x/sys/unix"
	"net"
	"os"
)
type fileConn struct {
	net.Conn // net.FileConn
	f *os.File
}

func (c fileConn) Close() error {
	if err := c.Conn.Close(); err != nil {
		return err
	}
	if err := c.f.Close(); err != nil {
		return err
	}
	return nil
}

func SocketPair() (a, b net.Conn, err error) {
	// don't use net.Pipe, as it doesn't implement things like lingering, which our code relies on
	sockpair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, nil, err
	}
	toConn := func(fd int) (net.Conn, error) {
		f := os.NewFile(uintptr(fd), "fileconn")
		if f == nil {
			panic(fd)
		}
		c, err := net.FileConn(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return fileConn{Conn: c, f: f}, nil
	}
	if a, err = toConn(sockpair[0]); err != nil { // shadowing
		return nil, nil, err
	}
	if b, err = toConn(sockpair[1]); err != nil { // shadowing
		a.Close()
		return nil, nil, err
	}
	return a, b, nil
}
