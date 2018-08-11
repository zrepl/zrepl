package replication

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestFilesystemVersion_RelName(t *testing.T) {

	type TestCase struct {
		In FilesystemVersion
		Out string
		Panic bool
	}

	tcs := []TestCase{
		{
			In: FilesystemVersion{
				Type: FilesystemVersion_Snapshot,
				Name: "foobar",
			},
			Out: "@foobar",
		},
		{
			In: FilesystemVersion{
				Type: FilesystemVersion_Bookmark,
				Name: "foobar",
			},
			Out: "#foobar",
		},
		{
			In: FilesystemVersion{
				Type: 2342,
				Name: "foobar",
			},
			Panic: true,
		},
	}

	for _, tc := range tcs {
		if tc.Panic {
			assert.Panics(t, func() {
				tc.In.RelName()
			})
		} else {
			o := tc.In.RelName()
			assert.Equal(t, tc.Out, o)
		}
	}

}

func TestFilesystemVersion_ZFSFilesystemVersion(t *testing.T) {

	empty := &FilesystemVersion{}
	emptyZFS := empty.ZFSFilesystemVersion()
	assert.Zero(t, emptyZFS.Creation)

	dateInvalid := &FilesystemVersion{Creation:"foobar"}
	assert.Panics(t, func() {
		dateInvalid.ZFSFilesystemVersion()
	})

}
