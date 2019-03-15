package socketpair

import (
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func SocketPair() (a, b *net.UnixConn, err error) {
	// don't use net.Pipe, as it doesn't implement things like lingering, which our code relies on
	sockpair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, nil, err
	}
	toConn := func(fd int) (*net.UnixConn, error) {
		f := os.NewFile(uintptr(fd), "fileconn")
		if f == nil {
			panic(fd)
		}
		c, err := net.FileConn(f)
		f.Close() // net.FileConn uses dup under the hood
		if err != nil {
			return nil, err
		}
		// strictly, the following type assertion is an implementation detail
		// however, will be caught by test TestSocketPairWorks
		fileConnIsUnixConn := c.(*net.UnixConn)
		return fileConnIsUnixConn, nil
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
