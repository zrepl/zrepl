package bytecounter

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zrepl/zrepl/zfs"
)

type mockStreamCopierAndReader struct {
	zfs.StreamCopier // to satisfy interface
	reads            int
}

func (r *mockStreamCopierAndReader) Read(p []byte) (int, error) {
	r.reads++
	return len(p), nil
}

var _ io.Reader = &mockStreamCopierAndReader{}

func TestNewStreamCopierReexportsReader(t *testing.T) {
	mock := &mockStreamCopierAndReader{}
	x := NewStreamCopier(mock)

	r, ok := x.(io.Reader)
	if !ok {
		t.Fatalf("%T does not implement io.Reader, hence reader cannout have been wrapped", x)
	}

	var buf [23]byte
	n, err := r.Read(buf[:])
	assert.True(t, mock.reads == 1)
	assert.True(t, n == len(buf))
	assert.NoError(t, err)
	assert.True(t, x.Count() == 23)
}
