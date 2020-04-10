package bytecounter

import (
	"io"
	"sync/atomic"
)

// ReadCloser wraps an io.ReadCloser, reimplementing
// its interface and counting the bytes written to during copying.
type ReadCloser interface {
	io.ReadCloser
	Count() int64
}

// NewReadCloser wraps rc.
func NewReadCloser(rc io.ReadCloser) ReadCloser {
	return &readCloser{rc, 0}
}

type readCloser struct {
	rc    io.ReadCloser
	count int64
}

func (r *readCloser) Count() int64 {
	return atomic.LoadInt64(&r.count)
}

var _ io.ReadCloser = &readCloser{}

func (r *readCloser) Close() error {
	return r.rc.Close()
}

func (r *readCloser) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	atomic.AddInt64(&r.count, int64(n))
	return n, err
}
