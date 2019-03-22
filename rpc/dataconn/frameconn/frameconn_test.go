package frameconn

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPublicFrameType(t *testing.T) {
	for i := uint32(0); i < 256; i++ {
		i := i
		t.Run(fmt.Sprintf("^%d", i), func(t *testing.T) {
			assert.False(t, IsPublicFrameType(^i))
		})
	}
	assert.True(t, IsPublicFrameType(0))
	assert.True(t, IsPublicFrameType(1))
	assert.True(t, IsPublicFrameType(255))
	assert.False(t, IsPublicFrameType(rstFrameType))
}
