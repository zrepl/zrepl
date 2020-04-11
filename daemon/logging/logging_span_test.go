package logging

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCallerOrPanic(t *testing.T) {
	withStackFromCtxMock := func() string {
		return getMyCallerOrPanic()
	}
	ret := withStackFromCtxMock()
	// zrepl prefix is stripped
	assert.Equal(t, "daemon/logging.TestGetCallerOrPanic", ret)
}
