package stream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

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

	// readMtx serializes read stream operations because we inherently only
	// support a single stream at a time over hc.
	readMtx            sync.Mutex
	readClean          bool
	allowWriteStreamTo bool

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

var ErrReadFramesStopped = fmt.Errorf("stream: reading frames stopped")

func (c *Conn) readFrames() {
	defer close(c.waitReadFramesDone)
	defer close(c.frameReads)
	readFrames(c.frameReads, c.hc)
}

func (c *Conn) ReadStreamedMessage(ctx context.Context, maxSize uint32, frameType uint32) ([]byte, *ReadStreamError) {
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
	err := readStream(c.frameReads, c.hc, w, frameType)
	c.readClean = isConnCleanAfterRead(err)
	w.CloseWithError(readMessageSentinel)
	wg.Wait()
	if err != nil {
		return nil, err
	} else {
		return buf.Bytes(), nil
	}
}

// WriteStreamTo reads a stream from Conn and writes it to w.
func (c *Conn) ReadStreamInto(w io.Writer, frameType uint32) zfs.StreamCopierError {
	c.readMtx.Lock()
	defer c.readMtx.Unlock()
	if !c.readClean {
		return writeStreamToErrorUnknownState{}
	}
	var err *ReadStreamError = readStream(c.frameReads, c.hc, w, frameType)
	c.readClean = isConnCleanAfterRead(err)

	// https://golang.org/doc/faq#nil_error
	if err == nil {
		return nil
	}
	return err
}

func (c *Conn) WriteStreamedMessage(ctx context.Context, buf io.Reader, frameType uint32) error {
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

func (c *Conn) SendStream(ctx context.Context, src zfs.StreamCopier, frameType uint32) error {
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
			w.CloseWithError(res.errStream)
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

func (c *Conn) Close() error {
	err := c.hc.Shutdown()
	<-c.waitReadFramesDone
	return err
}
