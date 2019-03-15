package tls

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
)

// adapts a tls.Conn and its underlying net.TCPConn into a valid transport.Wire
type transportWireAdaptor struct {
	*tls.Conn
	tcpConn *net.TCPConn
}

func newWireAdaptor(tlsConn *tls.Conn, tcpConn *net.TCPConn) transportWireAdaptor {
	return transportWireAdaptor{tlsConn, tcpConn}
}

// CloseWrite implements transport.Wire.CloseWrite which is different from *tls.Conn.CloseWrite:
// the former requires that the other side observes io.EOF, but *tls.Conn.CloseWrite does not
// close the underlying connection so no io.EOF would be observed.
func (w transportWireAdaptor) CloseWrite() error {
	if err := w.Conn.CloseWrite(); err != nil {
		// TODO log error
		fmt.Fprintf(os.Stderr, "transport/tls.CloseWrite() error: %s\n", err)
	}
	return w.tcpConn.CloseWrite()
}

// Close implements transport.Wire.Close which is different from a *tls.Conn.Close:
// At the time of writing (Go 1.11), closing tls.Conn closes the TCP connection immediately,
// which results in io.ErrUnexpectedEOF on the other side.
// We assume that w.Conn has a deadline set for the close, so the CloseWrite will time out if it blocks,
// falling through to the actual Close()
func (w transportWireAdaptor) Close() error {
	//	var buf [1<<15]byte
	//	w.Conn.Write(buf[:])
	// CloseWrite will send a TLS alert record down the line which
	// in the Go implementation acts like a flush...?
	//	if err := w.Conn.CloseWrite(); err != nil {
	//		// TODO log error
	//		fmt.Fprintf(os.Stderr, "transport/tls.Close() close write error: %s\n", err)
	//	}
	//	time.Sleep(1 * time.Second)
	return w.Conn.Close()
}
