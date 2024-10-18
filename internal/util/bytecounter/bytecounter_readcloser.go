package bytecounter

import (
	"io"
	"sync/atomic"
)

// ReadCloser wraps an io.ReadCloser, reimplementing
// its interface and counting the bytes written to during copying.
type ReadCloser interface {
	io.ReadCloser
	Count() uint64
}

// NewReadCloser wraps rc.
func NewReadCloser(rc io.ReadCloser) ReadCloser {
	return &readCloser{rc, 0}
}

type readCloser struct {
	rc    io.ReadCloser
	count uint64
}

func (r *readCloser) Count() uint64 {
	return atomic.LoadUint64(&r.count)
}

var _ io.ReadCloser = &readCloser{}

func (r *readCloser) Close() error {
	return r.rc.Close()
}

func (r *readCloser) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if n < 0 {
		panic("expecting n >= 0")
	}
	atomic.AddUint64(&r.count, uint64(n))
	return n, err
}
