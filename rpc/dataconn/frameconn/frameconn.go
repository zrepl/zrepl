package frameconn

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/rpc/dataconn/base2bufpool"
	"github.com/zrepl/zrepl/rpc/dataconn/timeoutconn"
)

type FrameHeader struct {
	Type       uint32
	PayloadLen uint32
}

// The 4 MSBs of ft are reserved for frameconn.
func IsPublicFrameType(ft uint32) bool {
	return (0xf<<28)&ft == 0
}

const (
	rstFrameType uint32 = 1<<28 + iota
)

func assertPublicFrameType(frameType uint32) {
	if !IsPublicFrameType(frameType) {
		panic(fmt.Sprintf("frameconn: frame type %v cannot be used by consumers of this package", frameType))
	}
}

func (f *FrameHeader) Unmarshal(buf []byte) {
	if len(buf) != 8 {
		panic("frame header is 8 bytes long")
	}
	f.Type = binary.BigEndian.Uint32(buf[0:4])
	f.PayloadLen = binary.BigEndian.Uint32(buf[4:8])
}

type Conn struct {
	readMtx, writeMtx sync.Mutex
	nc                *timeoutconn.Conn
	readNextValid     bool
	readNext          FrameHeader
	nextReadErr       error
	bufPool           *base2bufpool.Pool // no need for sync around it
	shutdown          shutdownFSM
}

func Wrap(nc *timeoutconn.Conn) *Conn {
	return &Conn{
		nc: nc,
		//		ncBuf: bufio.NewReadWriter(bufio.NewReaderSize(nc, 1<<23), bufio.NewWriterSize(nc, 1<<23)),
		bufPool:       base2bufpool.New(15, 22, base2bufpool.Allocate), // FIXME switch to Panic, but need to enforce the limits in recv for that. => need frameconn config
		readNext:      FrameHeader{},
		readNextValid: false,
	}
}

var ErrReadFrameLengthShort = errors.New("read frame length too short")
var ErrFixedFrameLengthMismatch = errors.New("read frame length mismatch")

type Buffer struct {
	bufpoolBuffer base2bufpool.Buffer
	payloadLen    uint32
}

func (b *Buffer) Free() {
	b.bufpoolBuffer.Free()
}

func (b *Buffer) Bytes() []byte {
	return b.bufpoolBuffer.Bytes()[0:b.payloadLen]
}

type Frame struct {
	Header FrameHeader
	Buffer Buffer
}

var ErrShutdown = fmt.Errorf("frameconn: shutting down")

// ReadFrame reads a frame from the connection.
//
// Due to an internal optimization (Readv, specifically), it is not guaranteed that a single call to
// WriteFrame unblocks a pending ReadFrame on an otherwise idle (empty) connection.
// The only way to guarantee that all previously written frames can reach the peer's layers on top
// of frameconn is to send an empty frame (no payload) and to ignore empty frames on the receiving side.
func (c *Conn) ReadFrame() (Frame, error) {

	if c.shutdown.IsShuttingDown() {
		return Frame{}, ErrShutdown
	}

	// only acquire readMtx now to prioritize the draining in Shutdown()
	// over external callers (= drain public callers)

	c.readMtx.Lock()
	defer c.readMtx.Unlock()
	f, err := c.readFrame()
	if f.Header.Type == rstFrameType {
		c.shutdown.Begin()
		return Frame{}, ErrShutdown
	}
	return f, err
}

// callers must have readMtx locked
func (c *Conn) readFrame() (Frame, error) {

	if c.nextReadErr != nil {
		ret := c.nextReadErr
		c.nextReadErr = nil
		return Frame{}, ret
	}

	if !c.readNextValid {
		var buf [8]byte
		if _, err := io.ReadFull(c.nc, buf[:]); err != nil {
			return Frame{}, err
		}
		c.readNext.Unmarshal(buf[:])
		c.readNextValid = true
	}

	// read payload + next header
	var nextHdrBuf [8]byte
	buffer := c.bufPool.Get(uint(c.readNext.PayloadLen))
	bufferBytes := buffer.Bytes()

	if c.readNext.PayloadLen == 0 {
		// This if statement implements the unlock-by-sending-empty-frame behavior
		// documented in ReadFrame's public docs.
		//
		// It is crucial that we return this empty frame now:
		// Consider the following plot with x-axis being time,
		// P being a frame with payload, E one without, X either of P or E
		//
		//    P P P P P P P E.....................X
		//                | |         |           |
		//                | |         |           F3
		//                | |         |
		//                | F2        |significant time between frames because
		//                F1           the peer has nothing to say to us
		//
		// Assume we're at the point were F2's header is in c.readNext.
		// That means F2 has not yet been returned.
		// But because it is empty (no payload), we're already done reading it.
		// If we omitted this if statement, the following would happen:
		// Readv below would read [][]byte{[len(0)], [len(8)]).

		c.readNextValid = false
		frame := Frame{
			Header: c.readNext,
			Buffer: Buffer{
				bufpoolBuffer: buffer,
				payloadLen:    c.readNext.PayloadLen, // 0
			},
		}
		return frame, nil
	}

	noNextHeader := false
	if n, err := c.nc.ReadvFull([][]byte{bufferBytes, nextHdrBuf[:]}); err != nil {
		noNextHeader = true
		zeroPayloadAndPeerClosed := n == 0 && c.readNext.PayloadLen == 0 && err == io.EOF
		zeroPayloadAndNextFrameHeaderThenPeerClosed := err == io.EOF && c.readNext.PayloadLen == 0 && n == int64(len(nextHdrBuf))
		nonzeroPayloadRecvdButNextHeaderMissing := n > 0 && uint32(n) == c.readNext.PayloadLen
		if zeroPayloadAndPeerClosed || zeroPayloadAndNextFrameHeaderThenPeerClosed || nonzeroPayloadRecvdButNextHeaderMissing {
			// This is the last frame on the conn.
			// Store the error to be returned on the next invocation of ReadFrame.
			c.nextReadErr = err
			// NORETURN, this frame is still valid
		} else {
			return Frame{}, err
		}
	}

	frame := Frame{
		Header: c.readNext,
		Buffer: Buffer{
			bufpoolBuffer: buffer,
			payloadLen:    c.readNext.PayloadLen,
		},
	}

	if !noNextHeader {
		c.readNext.Unmarshal(nextHdrBuf[:])
		c.readNextValid = true
	} else {
		c.readNextValid = false
	}

	return frame, nil
}

func (c *Conn) WriteFrame(payload []byte, frameType uint32) error {
	assertPublicFrameType(frameType)
	if c.shutdown.IsShuttingDown() {
		return ErrShutdown
	}
	c.writeMtx.Lock()
	defer c.writeMtx.Unlock()
	return c.writeFrame(payload, frameType)
}

func (c *Conn) writeFrame(payload []byte, frameType uint32) error {
	var hdrBuf [8]byte
	binary.BigEndian.PutUint32(hdrBuf[0:4], frameType)
	binary.BigEndian.PutUint32(hdrBuf[4:8], uint32(len(payload)))
	bufs := net.Buffers([][]byte{hdrBuf[:], payload})
	if _, err := c.nc.WritevFull(bufs); err != nil {
		return err
	}
	return nil
}

func (c *Conn) ResetWriteTimeout() error {
	return c.nc.RenewWriteDeadline()
}

func (c *Conn) Shutdown(deadline time.Time) error {
	// TCP connection teardown is a bit wonky if we are in a situation
	// where there is still data in flight (DIF) to our side:
	// If we just close the connection, our kernel will send RSTs
	// in response to the DIF, and those RSTs may reach the client's
	// kernel faster than the client app is able to pull the
	// last bytes from its kernel TCP receive buffer.
	//
	// Therefore, we send a frame with type rstFrameType to indicate
	// that the connection is to be closed immediately, and further
	// use CloseWrite instead of Close.
	// As per definition of the wire interface, CloseWrite guarantees
	// delivery of the data in our kernel TCP send buffer.
	// Therefore, the client always receives the RST frame.
	//
	// Now what are we going to do after that?
	//
	// 1. Naive Option: We just call Close() right after CloseWrite.
	// This yields the same race condition as explained above (DIF, first
	// paragraph): The situation just became a little more unlikely because
	// our rstFrameType + CloseWrite dance gave the client a full RTT worth of
	// time to read the data from its TCP recv buffer.
	//
	// 2. Correct Option: Drain the read side until io.EOF
	// We can read from the unclosed read-side of the connection until we get
	// the io.EOF caused by the (well behaved) client closing the connection
	// in response to it reading the rstFrameType frame we sent.
	// However, this wastes resources on our side (we don't care about the
	// pending data anymore), and has potential for (D)DoS through CPU-time
	// exhaustion if the client just keeps sending data.
	// Then again, this option has the advantage with well-behaved clients
	// that we do not waste precious kernel-memory on the stale receive buffer
	// on our side (which is still full of data that we do not intend to read).
	//
	// 2.1 DoS Mitigation: Bound the number of bytes to drain, then close
	// At the time of writing, this technique is practiced by the Go http server
	// implementation, and actually SHOULDed in the HTTP 1.1 RFC.  It is
	// important to disable the idle timeout of the underlying timeoutconn in
	// that case and set an absolute deadline by which the socket must have
	// been fully drained. Not too hard, though ;)
	//
	// 2.2: Client sends RST, not FIN when it receives an rstFrameTyp frame.
	// We can use wire.(*net.TCPConn).SetLinger(0) to force an RST to be sent
	// on a subsequent close (instead of a FIN + wait for FIN+ACK).
	// TODO put this into Wire interface as an abstract method.
	//
	// 2.3 Only start draining after N*RTT
	// We have an RTT approximation from Wire.CloseWrite, which by definition
	// must not return before all to-be-sent-data has been acknowledged by the
	// client. Give the client a fair chance to react, and only start draining
	// after a multiple of the RTT has elapsed.
	// We waste the recv buffer memory a little longer than necessary, iff the
	// client reacts faster than expected. But we don't wast CPU time.
	// If we apply 2.2, we'll also have the benefit that our kernel will have
	// dropped the recv buffer memory as soon as it receives the client's RST.
	//
	// 3. TCP-only: OOB-messaging
	// We can use TCP's 'urgent' flag in the client to acknowledge the receipt
	// of the rstFrameType to us.
	// We can thus wait for that signal while leaving the kernel buffer as is.

	// TODO: For now, we just drain the connection (Option 2),
	// but we enforce deadlines so the _time_ we drain the connection
	// is bounded, although we do _that_ at full speed

	defer prometheus.NewTimer(prom.ShutdownSeconds).ObserveDuration()

	closeWire := func(step string) error {
		// TODO SetLinger(0) or similar (we want RST frames here, not FINS)
		closeErr := c.nc.Close()
		if closeErr == nil {
			return nil
		}

		// TODO go1.13: https://github.com/zrepl/zrepl/issues/190
		//              https://github.com/golang/go/issues/8319
		// (use errors.Is(closeErr, syscall.ECONNRESET))
		if pe, ok := closeErr.(*net.OpError); ok && pe.Err == syscall.ECONNRESET {
			// connection reset by peer on FreeBSD, see https://github.com/zrepl/zrepl/issues/190
			// We know from kernel code reading that the FD behind c.nc is closed, so let's not consider this an error
			return nil
		}

		prom.ShutdownCloseErrors.WithLabelValues("close").Inc()
		return closeErr
	}

	hardclose := func(err error, step string) error {
		prom.ShutdownHardCloses.WithLabelValues(step).Inc()
		return closeWire(step)
	}

	c.shutdown.Begin()
	// new calls to c.ReadFrame and c.WriteFrame will now return ErrShutdown
	// Acquiring writeMtx and readMtx afterwards ensures that already-running calls exit successfully

	// disable renewing timeouts now, enforce the requested deadline instead
	// we need to do this before acquiring locks to enforce the timeout on slow
	// clients / if something hangs (DoS mitigation)
	if err := c.nc.DisableTimeouts(); err != nil {
		return hardclose(err, "disable_timeouts")
	}
	if err := c.nc.SetDeadline(deadline); err != nil {
		return hardclose(err, "set_deadline")
	}

	c.writeMtx.Lock()
	defer c.writeMtx.Unlock()

	if err := c.writeFrame([]byte{}, rstFrameType); err != nil {
		return hardclose(err, "write_frame")
	}

	if err := c.nc.CloseWrite(); err != nil {
		return hardclose(err, "close_write")
	}

	c.readMtx.Lock()
	defer c.readMtx.Unlock()

	// TODO DoS mitigation: wait for client acknowledgement that they initiated Shutdown,
	// then perform abortive close on our side. As explained above, probably requires
	// OOB signaling such as TCP's urgent flag => transport-specific?

	// TODO DoS mitigation by reading limited number of bytes
	// see discussion above why this is non-trivial
	defer prometheus.NewTimer(prom.ShutdownDrainSeconds).ObserveDuration()
	n, _ := io.Copy(ioutil.Discard, c.nc)
	prom.ShutdownDrainBytesRead.Observe(float64(n))

	return closeWire("close")
}
