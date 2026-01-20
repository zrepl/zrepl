package stream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/zrepl/zrepl/internal/logger"
	"github.com/zrepl/zrepl/internal/rpc/dataconn/base2bufpool"
	"github.com/zrepl/zrepl/internal/rpc/dataconn/frameconn"
	"github.com/zrepl/zrepl/internal/rpc/dataconn/heartbeatconn"
)

type Logger = logger.Logger

type contextKey int

const (
	contextKeyLogger contextKey = 1 + iota
)

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLogger, log)
}

//nolint:unused
func getLog(ctx context.Context) Logger {
	log, ok := ctx.Value(contextKeyLogger).(Logger)
	if !ok {
		log = logger.NewNullLogger()
	}
	return log
}

// Frame types used by this package.
// 4 MSBs are reserved for frameconn, next 4 MSB for heartbeatconn, next 4 MSB for us.
const (
	StreamErrTrailer uint32 = 1 << (16 + iota)
	End
	// max 16
)

// NOTE: make sure to add a tests for each frame type that checks
//       whether it is heartbeatconn.IsPublicFrameType()

// Check whether the given frame type is allowed to be used by
// consumers of this package. Intended for use in unit tests.
func IsPublicFrameType(ft uint32) bool {
	return frameconn.IsPublicFrameType(ft) && heartbeatconn.IsPublicFrameType(ft) && ((0xf<<16)&ft == 0)
}

const FramePayloadShift = 19

var bufpool = base2bufpool.New(FramePayloadShift, FramePayloadShift, base2bufpool.Panic)

// if sendStream returns an error, that error will be sent as a trailer to the client
// ok will return nil, though.
func writeStream(ctx context.Context, c *heartbeatconn.Conn, stream io.Reader, stype uint32) (errStream, errConn error) {
	debug("writeStream: enter stype=%v", stype)
	defer debug("writeStream: return")
	if stype == 0 {
		panic("stype must be non-zero")
	}
	if !IsPublicFrameType(stype) {
		panic(fmt.Sprintf("stype %v is not public", stype))
	}
	return doWriteStream(ctx, c, stream, stype)
}

func doWriteStream(ctx context.Context, c *heartbeatconn.Conn, stream io.Reader, stype uint32) (errStream, errConn error) {

	// RULE1 (buf == <zero>) XOR (err == nil)
	type read struct {
		buf base2bufpool.Buffer
		err error
	}

	var wg sync.WaitGroup
	defer wg.Wait()
	reads := make(chan read, 5)
	var stopReading uint32
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(reads)
		for atomic.LoadUint32(&stopReading) == 0 {
			buffer := bufpool.Get(1 << FramePayloadShift)
			bufferBytes := buffer.Bytes()
			n, err := io.ReadFull(stream, bufferBytes)
			buffer.Shrink(uint(n))
			// if we received anything, send one read without an error (RULE 1)
			if n > 0 {
				reads <- read{buffer, nil}
			}
			if err == io.ErrUnexpectedEOF {
				// happens iff io.ReadFull read io.EOF from stream
				err = io.EOF
			}
			if err != nil {
				reads <- read{err: err} // RULE1
				return
			}
		}
	}()

	defer func() {
		// stop reading
		atomic.StoreUint32(&stopReading, 1)
		// drain in-flight reads
		for read := range reads {
			debug("doWriteStream: drain read channel")
			read.buf.Free()
		}
	}()

	for read := range reads {
		if read.err == nil {
			// RULE 1: read.buf is valid
			// next line is the hot path...
			writeErr := c.WriteFrame(read.buf.Bytes(), stype)
			read.buf.Free()
			if writeErr != nil {
				return nil, writeErr
			}
			continue
		} else if read.err == io.EOF {
			if err := c.WriteFrame([]byte{}, End); err != nil {
				return nil, err
			}
			break
		} else {
			errReader := strings.NewReader(read.err.Error())
			errReadErrReader, errConnWrite := doWriteStream(ctx, c, errReader, StreamErrTrailer)
			if errReadErrReader != nil {
				panic(errReadErrReader) // in-memory, cannot happen
			}
			return read.err, errConnWrite
		}
	}

	return nil, nil
}

type ReadStreamErrorKind int

const (
	ReadStreamErrorKindConn ReadStreamErrorKind = 1 + iota
	ReadStreamErrorKindWrite
	ReadStreamErrorKindSource
	ReadStreamErrorKindStreamErrTrailerEncoding
	ReadStreamErrorKindUnexpectedFrameType
)

type ReadStreamError struct {
	Kind ReadStreamErrorKind
	Err  error
}

func (e *ReadStreamError) Error() string {
	kindStr := ""
	switch e.Kind {
	case ReadStreamErrorKindConn:
		kindStr = " read error: "
	case ReadStreamErrorKindWrite:
		kindStr = " write error: "
	case ReadStreamErrorKindSource:
		kindStr = " source error: "
	case ReadStreamErrorKindStreamErrTrailerEncoding:
		kindStr = " source implementation error: "
	case ReadStreamErrorKindUnexpectedFrameType:
		kindStr = " protocol error: "
	}
	return fmt.Sprintf("stream:%s%s", kindStr, e.Err)
}

var _ net.Error = &ReadStreamError{}

func (e ReadStreamError) Timeout() bool {
	if netErr, ok := e.Err.(net.Error); ok {
		return netErr.Timeout()
	}
	return false
}

// This function is deprecated in net.Error and since this
// function is not involved in .Accept() code path, nothing
// really needs this method to be here.
func (e ReadStreamError) Temporary() bool {
	if te, ok := e.Err.(interface{ Temporary() bool }); ok {
		return te.Temporary()
	}
	return false
}

var _ net.Error = &ReadStreamError{}

func (e ReadStreamError) IsReadError() bool {
	return e.Kind != ReadStreamErrorKindWrite
}

func (e ReadStreamError) IsWriteError() bool {
	return e.Kind == ReadStreamErrorKindWrite
}

type readFrameResult struct {
	f   frameconn.Frame
	err error
}

// readFrames reads from c into reads
// if a read from c encounters an error, noMoreReads is closed before sending the result into reads
func readFrames(reads chan<- readFrameResult, noMoreReads chan<- struct{}, c *heartbeatconn.Conn) {
	// noMoreReads is already closed, don't re-close it
	defer close(reads)
	for { // only exits after a read error, make sure noMoreReads is closed
		var r readFrameResult
		r.f, r.err = c.ReadFrame()
		if r.err != nil && noMoreReads != nil {
			close(noMoreReads)
		}
		reads <- r
		if r.err != nil {
			return
		}
	}
}

// ReadStream will close c if an error reading  from c or writing to receiver occurs
//
// readStream calls itself recursively to read multi-frame error trailers
// Thus, the reads channel needs to be a parameter.
func readStream(reads <-chan readFrameResult, c *heartbeatconn.Conn, receiver io.Writer, stype uint32) *ReadStreamError {

	var f frameconn.Frame
	for read := range reads {
		debug("readStream: read frame %v %v", read.f.Header, read.err)
		f = read.f
		if read.err != nil {
			return &ReadStreamError{ReadStreamErrorKindConn, read.err}
		}
		if f.Header.Type != stype {
			break
		}

		n, err := receiver.Write(f.Buffer.Bytes())
		if err != nil {
			f.Buffer.Free()
			return &ReadStreamError{ReadStreamErrorKindWrite, err} // FIXME wrap as writer error
		}
		if n != len(f.Buffer.Bytes()) {
			f.Buffer.Free()
			return &ReadStreamError{ReadStreamErrorKindWrite, io.ErrShortWrite}
		}
		f.Buffer.Free()
	}

	if f.Header.Type == End {
		debug("readStream: End reached")
		return nil
	}

	if f.Header.Type == StreamErrTrailer {
		debug("readStream: begin of StreamErrTrailer")
		var errBuf bytes.Buffer
		if n, err := errBuf.Write(f.Buffer.Bytes()); n != len(f.Buffer.Bytes()) || err != nil {
			panic(fmt.Sprintf("unexpected bytes.Buffer write error: %v %v", n, err))
		}
		// recursion ftw! we won't enter this if stmt because stype == StreamErrTrailer in the following call
		rserr := readStream(reads, c, &errBuf, StreamErrTrailer)
		if rserr != nil && rserr.Kind == ReadStreamErrorKindWrite {
			panic(fmt.Sprintf("unexpected bytes.Buffer write error: %s", rserr))
		} else if rserr != nil {
			debug("readStream: rserr != nil && != ReadStreamErrorKindWrite: %v %v\n", rserr.Kind, rserr.Err)
			return rserr
		}
		if !utf8.Valid(errBuf.Bytes()) {
			return &ReadStreamError{ReadStreamErrorKindStreamErrTrailerEncoding, fmt.Errorf("source error, but not encoded as UTF-8")}
		}
		return &ReadStreamError{ReadStreamErrorKindSource, fmt.Errorf("%s", errBuf.String())}
	}

	return &ReadStreamError{ReadStreamErrorKindUnexpectedFrameType, fmt.Errorf("unexpected frame type %v (expected %v)", f.Header.Type, stype)}
}
