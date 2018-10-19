package util

import (
	"io"
	"net"
	"os"
	"sync/atomic"
	"time"
)

type NetConnLogger struct {
	net.Conn
	ReadFile  *os.File
	WriteFile *os.File
}

func NewNetConnLogger(conn net.Conn, readlog, writelog string) (l *NetConnLogger, err error) {
	l = &NetConnLogger{
		Conn: conn,
	}
	flags := os.O_CREATE | os.O_WRONLY
	if readlog != "" {
		if l.ReadFile, err = os.OpenFile(readlog, flags, 0600); err != nil {
			return
		}
	}
	if writelog != "" {
		if l.WriteFile, err = os.OpenFile(writelog, flags, 0600); err != nil {
			return
		}
	}
	return
}

func (c *NetConnLogger) Read(buf []byte) (n int, err error) {
	n, err = c.Conn.Read(buf)
	if c.WriteFile != nil {
		if _, writeErr := c.ReadFile.Write(buf[0:n]); writeErr != nil {
			panic(writeErr)
		}
	}
	return
}

func (c *NetConnLogger) Write(buf []byte) (n int, err error) {
	n, err = c.Conn.Write(buf)
	if c.ReadFile != nil {
		if _, writeErr := c.WriteFile.Write(buf[0:n]); writeErr != nil {
			panic(writeErr)
		}
	}
	return
}
func (c *NetConnLogger) Close() (err error) {
	err = c.Conn.Close()
	if err != nil {
		return
	}
	if c.ReadFile != nil {
		if err := c.ReadFile.Close(); err != nil {
			panic(err)
		}
	}
	if c.WriteFile != nil {
		if err := c.WriteFile.Close(); err != nil {
			panic(err)
		}
	}
	return
}

type ChainedReader struct {
	Readers   []io.Reader
	curReader int
}

func NewChainedReader(reader ...io.Reader) *ChainedReader {
	return &ChainedReader{
		Readers:   reader,
		curReader: 0,
	}
}

func (c *ChainedReader) Read(buf []byte) (n int, err error) {

	n = 0

	for c.curReader < len(c.Readers) {
		n, err = c.Readers[c.curReader].Read(buf)
		if err == io.EOF {
			c.curReader++
			continue
		}
		break
	}
	if c.curReader == len(c.Readers) {
		err = io.EOF // actually, there was no gap
	}

	return
}

type ByteCounterReader struct {
	reader    io.ReadCloser

	// called & accessed synchronously during Read, no external access
	cb        func(full int64)
	cbEvery   time.Duration
	lastCbAt  time.Time
	bytesSinceLastCb int64

	// set atomically because it may be read by multiple threads
	bytes     int64
}

func NewByteCounterReader(reader io.ReadCloser) *ByteCounterReader {
	return &ByteCounterReader{
		reader: reader,
	}
}

func (b *ByteCounterReader) SetCallback(every time.Duration, cb func(full int64)) {
	b.cbEvery = every
	b.cb = cb
}

func (b *ByteCounterReader) Close() error {
	return b.reader.Close()
}

func (b *ByteCounterReader) Read(p []byte) (n int, err error) {
	n, err = b.reader.Read(p)
	full := atomic.AddInt64(&b.bytes, int64(n))
	now := time.Now()
	if b.cb != nil && now.Sub(b.lastCbAt) > b.cbEvery {
		b.cb(full)
		b.lastCbAt = now
	}
	return n, err
}

func (b *ByteCounterReader) Bytes() int64 {
	return atomic.LoadInt64(&b.bytes)
}
