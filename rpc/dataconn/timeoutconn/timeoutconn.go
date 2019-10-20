// package timeoutconn wraps a Wire to provide idle timeouts
// based on Set{Read,Write}Deadline.
// Additionally, it exports abstractions for vectored I/O.
package timeoutconn

// NOTE
// Readv and Writev are not split-off into a separate package
// because we use raw syscalls, bypassing Conn's Read / Write methods.

import (
	"errors"
	"io"
	"net"
	"sync/atomic"
	"syscall"
	"time"
)

type Wire interface {
	net.Conn
	// A call to CloseWrite indicates that no further Write calls will be made to Wire.
	// The implementation must return an error in case of Write calls after CloseWrite.
	// On the peer's side, after it read all data written to Wire prior to the call to
	// CloseWrite on our side, the peer's Read calls must return io.EOF.
	// CloseWrite must not affect the read-direction of Wire: specifically, the
	// peer must continue to be able to send, and our side must continue be
	// able to receive data over Wire.
	//
	// Note that CloseWrite may (and most likely will) return sooner than the
	// peer having received all data written to Wire prior to CloseWrite.
	// Note further that buffering happening in the network stacks on either side
	// mandates an explicit acknowledgement from the peer that the connection may
	// be fully shut down: If we call Close without such acknowledgement, any data
	// from peer to us that was already in flight may cause connection resets to
	// be sent from us to the peer via the specific transport protocol. Those
	// resets (e.g. RST frames) may erase all connection context on the peer,
	// including data in its receive buffers. Thus, those resets are in race with
	// a) transmission of data written prior to CloseWrite and
	// b) the peer application reading from those buffers.
	//
	// The WaitForPeerClose method can be used to wait for connection termination,
	// iff the implementation supports it. If it does not, the only reliable way
	// to wait for a peer to have read all data from Wire (until io.EOF), is to
	// expect it to close the wire at that point as well, and to drain Wire until
	// we also read io.EOF.
	CloseWrite() error

	// Wait for the peer to close the connection.
	// No data that could otherwise be Read is lost as a consequence of this call.
	// The use case for this API is abortive connection shutdown.
	// To provide any value over draining Wire using io.Read, an implementation
	// will likely use out-of-bounds messaging mechanisms.
	// TODO WaitForPeerClose() (supported bool, err error)
}

type Conn struct {
	Wire
	renewDeadlinesDisabled int32
	idleTimeout            time.Duration
}

func Wrap(conn Wire, idleTimeout time.Duration) Conn {
	return Conn{Wire: conn, idleTimeout: idleTimeout}
}

// DisableTimeouts disables the idle timeout behavior provided by this package.
// Existing deadlines are cleared iff the call is the first call to this method.
func (c *Conn) DisableTimeouts() error {
	if atomic.CompareAndSwapInt32(&c.renewDeadlinesDisabled, 0, 1) {
		return c.SetDeadline(time.Time{})
	}
	return nil
}

func (c *Conn) renewReadDeadline() error {
	if atomic.LoadInt32(&c.renewDeadlinesDisabled) != 0 {
		return nil
	}
	return c.SetReadDeadline(time.Now().Add(c.idleTimeout))
}

func (c *Conn) RenewWriteDeadline() error {
	if atomic.LoadInt32(&c.renewDeadlinesDisabled) != 0 {
		return nil
	}
	return c.SetWriteDeadline(time.Now().Add(c.idleTimeout))
}

func (c Conn) Read(p []byte) (n int, err error) {
	n = 0
	err = nil
restart:
	if err := c.renewReadDeadline(); err != nil {
		return n, err
	}
	var nCurRead int
	nCurRead, err = c.Wire.Read(p[n:])
	n += nCurRead
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() && nCurRead > 0 {
		err = nil
		goto restart
	}
	return n, err
}

func (c Conn) Write(p []byte) (n int, err error) {
	n = 0
restart:
	if err := c.RenewWriteDeadline(); err != nil {
		return n, err
	}
	var nCurWrite int
	nCurWrite, err = c.Wire.Write(p[n:])
	n += nCurWrite
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() && nCurWrite > 0 {
		err = nil
		goto restart
	}
	return n, err
}

// Writes the given buffers to Conn, following the sematincs of io.Copy,
// but is guaranteed to use the writev system call if the wrapped Wire
// support it.
// Note the Conn does not support writev through io.Copy(aConn, aNetBuffers).
func (c Conn) WritevFull(bufs net.Buffers) (n int64, err error) {
	n = 0
restart:
	if err := c.RenewWriteDeadline(); err != nil {
		return n, err
	}
	var nCurWrite int64
	nCurWrite, err = io.Copy(c.Wire, &bufs)
	n += nCurWrite
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() && nCurWrite > 0 {
		err = nil
		goto restart
	}
	return n, err
}

var SyscallConnNotSupported = errors.New("SyscallConn not supported")

// The interface that must be implemented for vectored I/O support.
// If the wrapped Wire does not implement it, a less efficient
// fallback implementation is used.
// Rest assured that Go's *net.TCPConn implements this interface.
type SyscallConner interface {
	// The sentinel error value SyscallConnNotSupported can be returned
	// if the support for SyscallConn depends on runtime conditions and
	// that runtime condition is not met.
	SyscallConn() (syscall.RawConn, error)
}

var _ SyscallConner = (*net.TCPConn)(nil)

// Reads the given buffers full:
// Think of io.ReadvFull, but for net.Buffers + using the readv syscall.
//
// If the underlying Wire is not a SyscallConner, a fallback
// ipmlementation based on repeated Conn.Read invocations is used.
//
// If the connection returned io.EOF, the number of bytes up ritten until
// then + io.EOF is returned. This behavior is different to io.ReadFull
// which returns io.ErrUnexpectedEOF.
func (c Conn) ReadvFull(buffers net.Buffers) (n int64, err error) {
	return c.readv(buffers)
}

// invoked by c.readv if readv system call cannot be used
func (c Conn) readvFallback(nbuffers net.Buffers) (n int64, err error) {
	buffers := [][]byte(nbuffers)
	for i := range buffers {
		curBuf := buffers[i]
	inner:
		for len(curBuf) > 0 {
			if err := c.renewReadDeadline(); err != nil {
				return n, err
			}
			var oneN int
			oneN, err = c.Read(curBuf[:]) // WE WANT NO SHADOWING
			curBuf = curBuf[oneN:]
			n += int64(oneN)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() && oneN > 0 {
					continue inner
				}
				return n, err
			}
		}
	}
	return n, nil
}
