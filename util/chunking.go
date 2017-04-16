package chunking

import (
	"io"
	"encoding/binary"
	"bytes"
)

var ChunkBufSize uint32 = 32*1024
var ChunkHeaderByteOrder = binary.LittleEndian

type Unchunker struct {
	ChunkCount int
	in 			io.Reader
	remainingChunkBytes uint32
}

func NewUnchunker(conn io.Reader) *Unchunker {
	return &Unchunker{
			in: conn,
			remainingChunkBytes: 0,
	}
}

func (c *Unchunker) Read(b []byte) (n int, err error) {

	if c.remainingChunkBytes == 0 {

		var nextChunkLen uint32
		err = binary.Read(c.in, ChunkHeaderByteOrder, &nextChunkLen)
		if err != nil {
			return
		}

		// A chunk of len 0 indicates end of stream
		if nextChunkLen == 0 {
			return 0, io.EOF
		}

		c.remainingChunkBytes = nextChunkLen
		c.ChunkCount++

	}

	maxRead := min(int(c.remainingChunkBytes), len(b))
	if maxRead < 0 {
		panic("Cannot read negative amount of bytes")
	}
	if maxRead == 0 {
		return 0, nil
	}

	n, err = c.in.Read(b[0:maxRead])
	if err != nil  {
		return n, err
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
	ChunkCount int
	in io.Reader
	remainingChunkBytes int
	payloadBuf []byte
	headerBuf *bytes.Buffer
}

func NewChunker(conn io.Reader) Chunker {
	return NewChunkerSized(conn, ChunkBufSize)
}

func NewChunkerSized(conn io.Reader, chunkSize uint32) Chunker {

	buf := make([]byte, int(chunkSize)-binary.Size(chunkSize))

	return Chunker{
		in: conn,
		remainingChunkBytes: 0,
		payloadBuf: buf,
		headerBuf: &bytes.Buffer{},

	}

}

func (c *Chunker) Read(b []byte) (n int, err error) {

	//fmt.Printf("chunker: c.remainingChunkBytes: %d len(b): %d\n", c.remainingChunkBytes, len(b))

	n = 0
	if c.remainingChunkBytes == 0 {

		newPayloadLen, err := c.in.Read(c.payloadBuf)

		if newPayloadLen == 0 {
			return 0, io.EOF
		} else if err != nil {
			return newPayloadLen, err
		}

		c.remainingChunkBytes = newPayloadLen

		// Write chunk header
		c.headerBuf.Reset()
		nextChunkLen := uint32(newPayloadLen);
		headerLen := binary.Size(nextChunkLen)
		err = binary.Write(c.headerBuf, ChunkHeaderByteOrder, nextChunkLen)
		if err != nil {
			return n, err
		}
		copy(b[0:headerLen], c.headerBuf.Bytes())
		n += headerLen
		c.ChunkCount++
	}

	remainingBuf := b[n:]
	n2 := copy(remainingBuf, c.payloadBuf[:c.remainingChunkBytes])
	//fmt.Printf("chunker: written: %d\n", n+int(n2))
	c.remainingChunkBytes -= n2
	return n+int(n2), err
}