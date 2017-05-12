package util

import (
	"bytes"
	"encoding/binary"
	"io"
)

var ChunkBufSize uint32 = 32 * 1024
var ChunkHeaderByteOrder = binary.LittleEndian

type Unchunker struct {
	ChunkCount          int
	in                  io.Reader
	remainingChunkBytes uint32
	finishErr           error
}

func NewUnchunker(conn io.Reader) *Unchunker {
	return &Unchunker{
		in:                  conn,
		remainingChunkBytes: 0,
	}
}

func (c *Unchunker) Read(b []byte) (n int, err error) {

	if c.finishErr != nil {
		return 0, err
	}

	if c.remainingChunkBytes == 0 {

		var nextChunkLen uint32
		err = binary.Read(c.in, ChunkHeaderByteOrder, &nextChunkLen)
		if err != nil {
			c.finishErr = err // can't handle this
			return
		}

		// A chunk of len 0 indicates end of stream
		if nextChunkLen == 0 {
			c.finishErr = io.EOF
			return 0, c.finishErr
		}

		c.remainingChunkBytes = nextChunkLen
		c.ChunkCount++

	}

	if c.remainingChunkBytes <= 0 {
		panic("internal inconsistency: c.remainingChunkBytes must be > 0")
	}
	if len(b) <= 0 {
		panic("cannot read into buffer of length 0")
	}

	maxRead := min(int(c.remainingChunkBytes), len(b))

	n, err = c.in.Read(b[0:maxRead])
	if err != nil {
		return
	}
	c.remainingChunkBytes -= uint32(n)

	return

}

func min(a, b int) int {
	if a < b {
		return a
	} else {
		return b
	}
}

type Chunker struct {
	ChunkCount          int
	in                  io.Reader
	inEOF               bool
	remainingChunkBytes int
	payloadBufLen       int
	payloadBuf          []byte
	headerBuf           *bytes.Buffer
	finalHeaderBuffered bool
}

func NewChunker(conn io.Reader) Chunker {
	return NewChunkerSized(conn, ChunkBufSize)
}

func NewChunkerSized(conn io.Reader, chunkSize uint32) Chunker {

	buf := make([]byte, int(chunkSize)-binary.Size(chunkSize))

	return Chunker{
		in:                  conn,
		remainingChunkBytes: 0,
		payloadBufLen:       0,
		payloadBuf:          buf,
		headerBuf:           &bytes.Buffer{},
	}

}

func (c *Chunker) Read(b []byte) (n int, err error) {

	if len(b) == 0 {
		panic("unexpected empty output buffer")
	}

	if c.inEOF && c.finalHeaderBuffered && c.headerBuf.Len() == 0 { // all bufs empty and no more bytes to expect
		return 0, io.EOF
	}

	n = 0

	if c.headerBuf.Len() > 0 { // first drain the header buf
		nh, err := c.headerBuf.Read(b[n:])
		if nh > 0 {
			n += nh
		}
		if nh == 0 || (err != nil && err != io.EOF) {
			panic("unexpected behavior: in-memory buffer should not throw errors")
		}
		if c.headerBuf.Len() != 0 {
			return n, nil // finish writing the header before we can proceed with payload
		}
		if c.finalHeaderBuffered {
			// we just wrote the final header
			return n, io.EOF
		}
	}

	if c.remainingChunkBytes > 0 { // then drain the payload buf

		npl := copy(b[n:], c.payloadBuf[c.payloadBufLen-c.remainingChunkBytes:c.payloadBufLen])
		c.remainingChunkBytes -= npl
		if c.remainingChunkBytes < 0 {
			panic("unexpected behavior, copy() should not copy more than max(cap(), len())")
		}
		n += npl
	}

	if c.remainingChunkBytes == 0 && !c.inEOF { // fillup bufs

		newPayloadLen, err := c.in.Read(c.payloadBuf)

		if newPayloadLen > 0 {
			c.payloadBufLen = newPayloadLen
			c.remainingChunkBytes = newPayloadLen
		}
		if err == io.EOF {
			c.inEOF = true
		} else if err != nil {
			return n, err
		}
		if newPayloadLen == 0 { // apparently, this happens with some Readers
			c.finalHeaderBuffered = true
		}

		// Fill header buf
		{
			c.headerBuf.Reset()
			nextChunkLen := uint32(newPayloadLen)
			err := binary.Write(c.headerBuf, ChunkHeaderByteOrder, nextChunkLen)
			if err != nil {
				panic("unexpected error, write to in-memory buffer should not throw error")
			}
		}

		if c.headerBuf.Len() == 0 {
			panic("unexpected empty header buf")
		}

	} else if c.remainingChunkBytes == 0 && c.inEOF && !c.finalHeaderBuffered {

		c.headerBuf.Reset()
		err := binary.Write(c.headerBuf, ChunkHeaderByteOrder, uint32(0))
		if err != nil {
			panic("unexpected error, write to in-memory buffer should not throw error [2]")
		}
		c.finalHeaderBuffered = true

	}

	return

}
