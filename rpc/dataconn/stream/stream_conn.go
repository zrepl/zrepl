package stream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/rpc/dataconn/heartbeatconn"
	"github.com/zrepl/zrepl/rpc/dataconn/timeoutconn"
	"github.com/zrepl/zrepl/zfs"
)

type Conn struct {
	hc *heartbeatconn.Conn

	// whether the per-conn readFrames goroutine completed
	waitReadFramesDone chan struct{}
	// filled by per-conn readFrames goroutine
	frameReads chan readFrameResult

	closeState closeState

	// readMtx serializes read stream operations because we inherently only
	// support a single stream at a time over hc.
	readMtx   sync.Mutex
	readClean bool

	// writeMtx serializes write stream operations because we inherently only
	// support a single stream at a time over hc.
	writeMtx   sync.Mutex
	writeClean bool
}

var readMessageSentinel = fmt.Errorf("read stream complete")

type writeStreamToErrorUnknownState struct{}

func (e writeStreamToErrorUnknownState) Error() string {
	return "dataconn read stream: connection is in unknown state"
}

func (e writeStreamToErrorUnknownState) IsReadError() bool { return true }

func (e writeStreamToErrorUnknownState) IsWriteError() bool { return false }

func Wrap(nc timeoutconn.Wire, sendHeartbeatInterval, peerTimeout time.Duration) *Conn {
	hc := heartbeatconn.Wrap(nc, sendHeartbeatInterval, peerTimeout)
	conn := &Conn{
		hc: hc, readClean: true, writeClean: true,
		waitReadFramesDone: make(chan struct{}),
		frameReads:         make(chan readFrameResult, 5), // FIXME constant
	}
	go conn.readFrames()
	return conn
}

func isConnCleanAfterRead(res *ReadStreamError) bool {
	return res == nil || res.Kind == ReadStreamErrorKindSource || res.Kind == ReadStreamErrorKindStreamErrTrailerEncoding
}

func isConnCleanAfterWrite(err error) bool {
	return err == nil
}

func (c *Conn) readFrames() {
	readFrames(c.frameReads, c.waitReadFramesDone, c.hc)
}

func (c *Conn) ReadStreamedMessage(ctx context.Context, maxSize uint32, frameType uint32) (_ []byte, err *ReadStreamError) {

	// if we are closed while reading, return that as an error
	if closeGuard, cse := c.closeState.RWEntry(); cse != nil {
		return nil, &ReadStreamError{
			Kind: ReadStreamErrorKindConn,
			Err:  cse,
		}
	} else {
		defer func(err **ReadStreamError) {
			if closed := closeGuard.RWExit(); closed != nil {
				*err = &ReadStreamError{
					Kind: ReadStreamErrorKindConn,
					Err:  closed,
				}
			}
		}(&err)
	}

	c.readMtx.Lock()
	defer c.readMtx.Unlock()
	if !c.readClean {
		return nil, &ReadStreamError{
			Kind: ReadStreamErrorKindConn,
			Err:  fmt.Errorf("dataconn read message: connection is in unknown state"),
		}
	}

	r, w := io.Pipe()
	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		lr := io.LimitReader(r, int64(maxSize))
		if _, err := io.Copy(&buf, lr); err != nil && err != readMessageSentinel {
			panic(err)
		}
	}()
	err = readStream(c.frameReads, c.hc, w, frameType)
	c.readClean = isConnCleanAfterRead(err)
	_ = w.CloseWithError(readMessageSentinel) // always returns nil
	wg.Wait()
	if err != nil {
		return nil, err
	} else {
		return buf.Bytes(), nil
	}
}

// WriteStreamTo reads a stream from Conn and writes it to w.
func (c *Conn) ReadStreamInto(w io.Writer, frameType uint32) (err zfs.StreamCopierError) {

	// if we are closed while writing, return that as an error
	if closeGuard, cse := c.closeState.RWEntry(); cse != nil {
		return cse
	} else {
		defer func(err *zfs.StreamCopierError) {
			if closed := closeGuard.RWExit(); closed != nil {
				*err = closed
			}
		}(&err)
	}

	c.readMtx.Lock()
	defer c.readMtx.Unlock()
	if !c.readClean {
		return writeStreamToErrorUnknownState{}
	}
	var rse *ReadStreamError = readStream(c.frameReads, c.hc, w, frameType)
	c.readClean = isConnCleanAfterRead(rse)

	// https://golang.org/doc/faq#nil_error
	if rse == nil {
		return nil
	}
	return rse
}

func (c *Conn) WriteStreamedMessage(ctx context.Context, buf io.Reader, frameType uint32) (err error) {

	// if we are closed while writing, return that as an error
	if closeGuard, cse := c.closeState.RWEntry(); cse != nil {
		return cse
	} else {
		defer func(err *error) {
			if closed := closeGuard.RWExit(); closed != nil {
				*err = closed
			}
		}(&err)
	}

	c.writeMtx.Lock()
	defer c.writeMtx.Unlock()
	if !c.writeClean {
		return fmt.Errorf("dataconn write message: connection is in unknown state")
	}
	errBuf, errConn := writeStream(ctx, c.hc, buf, frameType)
	if errBuf != nil {
		panic(errBuf)
	}
	c.writeClean = isConnCleanAfterWrite(errConn)
	return errConn
}

func (c *Conn) SendStream(ctx context.Context, src zfs.StreamCopier, frameType uint32) (err error) {

	// if we are closed while reading, return that as an error
	if closeGuard, cse := c.closeState.RWEntry(); cse != nil {
		return cse
	} else {
		defer func(err *error) {
			if closed := closeGuard.RWExit(); closed != nil {
				*err = closed
			}
		}(&err)
	}

	c.writeMtx.Lock()
	defer c.writeMtx.Unlock()
	if !c.writeClean {
		return fmt.Errorf("dataconn send stream: connection is in unknown state")
	}

	// avoid io.Pipe if zfs.StreamCopier is an io.Reader
	var r io.Reader
	var w *io.PipeWriter
	streamCopierErrChan := make(chan zfs.StreamCopierError, 1)
	if reader, ok := src.(io.Reader); ok {
		r = reader
		streamCopierErrChan <- nil
		close(streamCopierErrChan)
	} else {
		r, w = io.Pipe()
		go func() {
			streamCopierErrChan <- src.WriteStreamTo(w)
			w.Close()
		}()
	}

	type writeStreamRes struct {
		errStream, errConn error
	}
	writeStreamErrChan := make(chan writeStreamRes, 1)
	go func() {
		var res writeStreamRes
		res.errStream, res.errConn = writeStream(ctx, c.hc, r, frameType)
		if w != nil {
			_ = w.CloseWithError(res.errStream) // always returns nil
		}
		writeStreamErrChan <- res
	}()

	writeRes := <-writeStreamErrChan
	streamCopierErr := <-streamCopierErrChan
	c.writeClean = isConnCleanAfterWrite(writeRes.errConn) // TODO correct?
	if streamCopierErr != nil && streamCopierErr.IsReadError() {
		return streamCopierErr // something on our side is bad
	} else {
		if writeRes.errStream != nil {
			return writeRes.errStream
		} else if writeRes.errConn != nil {
			return writeRes.errConn
		}
		// TODO combined error?
		return streamCopierErr
	}
}

type closeState struct {
	closeCount uint32
}

type closeStateErrConnectionClosed struct{}

var _ zfs.StreamCopierError = (*closeStateErrConnectionClosed)(nil)
var _ error = (*closeStateErrConnectionClosed)(nil)
var _ net.Error = (*closeStateErrConnectionClosed)(nil)

func (e *closeStateErrConnectionClosed) Error() string {
	return "connection closed"
}
func (e *closeStateErrConnectionClosed) IsReadError() bool  { return true }
func (e *closeStateErrConnectionClosed) IsWriteError() bool { return true }
func (e *closeStateErrConnectionClosed) Timeout() bool      { return false }
func (e *closeStateErrConnectionClosed) Temporary() bool    { return false }

func (s *closeState) CloseEntry() error {
	firstCloser := atomic.AddUint32(&s.closeCount, 1) == 1
	if !firstCloser {
		return errors.New("duplicate close")
	}
	return nil
}

type closeStateEntry struct {
	s          *closeState
	entryCount uint32
}

func (s *closeState) RWEntry() (e *closeStateEntry, err zfs.StreamCopierError) {
	entry := &closeStateEntry{s, atomic.LoadUint32(&s.closeCount)}
	if entry.entryCount > 0 {
		return nil, &closeStateErrConnectionClosed{}
	}
	return entry, nil
}

func (e *closeStateEntry) RWExit() zfs.StreamCopierError {
	if atomic.LoadUint32(&e.entryCount) == e.entryCount {
		// no calls to Close() while running rw operation
		return nil
	}
	return &closeStateErrConnectionClosed{}
}

func (c *Conn) Close() error {
	if err := c.closeState.CloseEntry(); err != nil {
		return errors.Wrap(err, "stream conn close")
	}

	// Shutdown c.hc, which will cause c.readFrames to close c.waitReadFramesDone
	err := c.hc.Shutdown()
	// However, c.readFrames may be blocking on a filled c.frameReads
	// and since the connection is closed, nobody is going to read from it
	for read := range c.frameReads {
		debug("Conn.Close() draining queued read")
		read.f.Buffer.Free()
	}
	// if we can't close, don't expect c.readFrames to terminate
	// this might leak c.readFrames, but we can't do something useful at this point
	if err != nil {
		return err
	}
	// shutdown didn't report errors, so c.readFrames will exit due to read error
	// This behavior is the contract we have with c.hc.
	// If that contract is broken, this read will block indefinitely and
	// cause an easily diagnosable goroutine leak (of this goroutine)
	<-c.waitReadFramesDone
	return nil
}
