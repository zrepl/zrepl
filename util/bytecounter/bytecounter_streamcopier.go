package bytecounter

import (
	"io"
	"sync/atomic"

	"github.com/zrepl/zrepl/zfs"
)

// StreamCopier wraps a zfs.StreamCopier, reimplemening
// its interface and counting the bytes written to during copying.
type StreamCopier interface {
	zfs.StreamCopier
	Count() int64
}

// NewStreamCopier wraps sc into a StreamCopier.
// If sc is io.Reader, it is guaranteed that the returned StreamCopier
// implements that interface, too.
func NewStreamCopier(sc zfs.StreamCopier) StreamCopier {
	bsc := &streamCopier{sc, 0}
	if scr, ok := sc.(io.Reader); ok {
		return streamCopierAndReader{bsc, scr}
	} else {
		return bsc
	}
}

type streamCopier struct {
	sc    zfs.StreamCopier
	count int64
}

// proxy writer used by streamCopier
type streamCopierWriter struct {
	parent *streamCopier
	w      io.Writer
}

func (w streamCopierWriter) Write(p []byte) (n int, err error) {
	n, err = w.w.Write(p)
	atomic.AddInt64(&w.parent.count, int64(n))
	return
}

func (s *streamCopier) Count() int64 {
	return atomic.LoadInt64(&s.count)
}

var _ zfs.StreamCopier = &streamCopier{}

func (s streamCopier) Close() error {
	return s.sc.Close()
}

func (s *streamCopier) WriteStreamTo(w io.Writer) zfs.StreamCopierError {
	ww := streamCopierWriter{s, w}
	return s.sc.WriteStreamTo(ww)
}

// a streamCopier whose underlying sc is an io.Reader
type streamCopierAndReader struct {
	*streamCopier
	asReader io.Reader
}

func (scr streamCopierAndReader) Read(p []byte) (int, error) {
	n, err := scr.asReader.Read(p)
	atomic.AddInt64(&scr.streamCopier.count, int64(n))
	return n, err
}
