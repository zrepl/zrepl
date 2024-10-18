package limitio

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockRC struct {
	r      io.Reader
	closed bool
}

func newMockRC(r io.Reader) *mockRC { return &mockRC{r, false} }

func (m mockRC) Read(b []byte) (int, error) {
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	return m.r.Read(b)
}

func (m *mockRC) Close() error {
	m.closed = true
	return nil
}

func TestReadCloser(t *testing.T) {
	foobarReader := bytes.NewReader([]byte("foobar2342"))
	mock := newMockRC(foobarReader)
	limited := ReadCloser(mock, 6)
	var buf [20]byte
	n, err := limited.Read(buf[:])
	assert.NoError(t, err)
	assert.Equal(t, 6, n)
	assert.Equal(t, buf[:n], []byte("foobar"))
	n, err = limited.Read(buf[:])
	assert.Equal(t, 0, n)
	assert.Equal(t, io.EOF, err)
	limited.Close()
	assert.True(t, mock.closed)
}
