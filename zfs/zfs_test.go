package zfs

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestZFSListHandlesProducesZFSErrorOnNonZeroExit(t *testing.T) {
	t.SkipNow() // FIXME ZFS_BINARY does not work if tests run in parallel

	var err error

	ZFS_BINARY = "./test_helpers/zfs_failer.sh"

	_, err = ZFSList([]string{"fictionalprop"}, "nonexistent/dataset")

	assert.Error(t, err)
	zfsError, ok := err.(ZFSError)
	assert.True(t, ok)
	assert.Equal(t, "error: this is a mock\n", string(zfsError.Stderr))
}

func TestDatasetPathTrimNPrefixComps(t *testing.T) {
	p, err := NewDatasetPath("foo/bar/a/b")
	assert.Nil(t, err)
	p.TrimNPrefixComps(2)
	assert.True(t, p.Equal(toDatasetPath("a/b")))
	p.TrimNPrefixComps((2))
	assert.True(t, p.Empty())
	p.TrimNPrefixComps((1))
	assert.True(t, p.Empty(), "empty trimming shouldn't do harm")
}

func TestZFSPropertySource(t *testing.T) {

	tcs := []struct{
		in zfsPropertySource
		exp []string
	}{
		{
			in: sourceAny,
			exp: []string{"local", "default", "inherited", "-", "temporary"},
		},
		{
			in: sourceTemporary,
			exp: []string{"temporary"},
		},
		{
			in: sourceLocal|sourceInherited,
			exp: []string{"local", "inherited"},
		},
	}

	toSet := func(in []string) map[string]struct{} {
		m := make(map[string]struct{}, len(in))
		for _, s := range in {
			m[s] = struct{}{}
		}
		return m
	}

	for _, tc := range tcs {

		res := tc.in.zfsGetSourceFieldPrefixes()
		resSet := toSet(res)
		expSet := toSet(tc.exp)
		assert.Equal(t, expSet, resSet)
	}

}
