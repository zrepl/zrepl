package bytecounter

import (
	"io"
	"sync/atomic"
	"time"
)

type ByteCounterReader struct {
	reader io.ReadCloser

	// called & accessed synchronously during Read, no external access
	cb       func(full int64)
	cbEvery  time.Duration
	lastCbAt time.Time

	// set atomically because it may be read by multiple threads
	bytes int64
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
