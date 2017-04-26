package zfs

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestZFSListHandlesProducesZFSErrorOnNonZeroExit(t *testing.T) {
	var err error

	ZFS_BINARY = "./test_helpers/zfs_failer.sh"

	_, err = zfsList("nonexistent/dataset", func(p DatasetPath) bool {
		return true
	})

	assert.Error(t, err)
	zfsError, ok := err.(ZFSError)
	assert.True(t, ok)
	assert.Equal(t, "error: this is a mock\n", string(zfsError.Stderr))
}
