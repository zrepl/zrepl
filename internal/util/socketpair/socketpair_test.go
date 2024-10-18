package socketpair

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// This is test is mostly to verify that the assumption about
// net.FileConn returning *net.UnixConn for AF_UNIX FDs works.
func TestSocketPairWorks(t *testing.T) {
	assert.NotPanics(t, func() {
		a, b, err := SocketPair()
		assert.NoError(t, err)
		a.Close()
		b.Close()
	})
}
