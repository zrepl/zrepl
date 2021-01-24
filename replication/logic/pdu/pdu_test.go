package pdu

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFilesystemVersion_RelName(t *testing.T) {

	type TestCase struct {
		In    *FilesystemVersion
		Out   string
		Panic bool
	}

	creat := FilesystemVersionCreation(time.Now())
	tcs := []TestCase{
		{
			In: &FilesystemVersion{
				Type:     FilesystemVersion_Snapshot,
				Name:     "foobar",
				Creation: creat,
			},
			Out: "@foobar",
		},
		{
			In: &FilesystemVersion{
				Type:     FilesystemVersion_Bookmark,
				Name:     "foobar",
				Creation: creat,
			},
			Out: "#foobar",
		},
		{
			In: &FilesystemVersion{
				Type:     2342,
				Name:     "foobar",
				Creation: creat,
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
	_, err := empty.ZFSFilesystemVersion()
	assert.Error(t, err)

	dateInvalid := &FilesystemVersion{Creation: "foobar"}
	_, err = dateInvalid.ZFSFilesystemVersion()
	assert.Error(t, err)

}
