package zfs

import (
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func fsvlist(fsv ...string) (r []FilesystemVersion) {

	r = make([]FilesystemVersion, len(fsv))
	for i, f := range fsv {
		if strings.HasPrefix(f, "#") {
			r[i] = FilesystemVersion{
				Name: strings.TrimPrefix(f, "#"),
				Type: Bookmark,
			}
		} else if strings.HasPrefix(f, "@") {
			r[i] = FilesystemVersion{
				Name: strings.TrimPrefix(f, "@"),
				Type: Snapshot,
			}
		} else {
			panic("invalid character")
		}
	}
	return
}

func doTest(left, right []FilesystemVersion, validate func(d FilesystemDiff)) {
	var d FilesystemDiff
	d = MakeFilesystemDiff(left, right)
	validate(d)
}

func TestMakeFilesystemDiff_IncrementalSnapshots(t *testing.T) {

	l := fsvlist

	// basic functionality
	doTest(l("@a", "@b"), l("@a", "@b", "@c", "@d"), func(d FilesystemDiff) {
		assert.Equal(t, l("@b", "@c", "@d"), d.IncrementalPath)
	})

	// no common ancestor
	doTest(l(), l("@a"), func(d FilesystemDiff) {
		assert.Nil(t, d.IncrementalPath)
		assert.False(t, d.Diverged)
		assert.Equal(t, l("@a"), d.MRCAPathRight)
	})
	doTest(l("@a", "@b"), l("@c", "@d"), func(d FilesystemDiff) {
		assert.Nil(t, d.IncrementalPath)
		assert.False(t, d.Diverged)
		assert.Equal(t, l("@c", "@d"), d.MRCAPathRight)
	})

	// divergence is detected
	doTest(l("@a", "@b1"), l("@a", "@b2"), func(d FilesystemDiff) {
		assert.Nil(t, d.IncrementalPath)
		assert.True(t, d.Diverged)
		assert.Equal(t, l("@a", "@b1"), d.MRCAPathLeft)
		assert.Equal(t, l("@a", "@b2"), d.MRCAPathRight)
	})

	// gaps before most recent common ancestor do not matter
	doTest(l("@a", "@b", "@c"), l("@a", "@c", "@d"), func(d FilesystemDiff) {
		assert.Equal(t, l("@c", "@d"), d.IncrementalPath)
	})

}

func TestMakeFilesystemDiff_BookmarksSupport(t *testing.T) {
	l := fsvlist

	// bookmarks are used
	doTest(l("@a"), l("#a", "@b"), func(d FilesystemDiff) {
		assert.Equal(t, l("#a", "@b"), d.IncrementalPath)
	})

	// boomarks are stripped from IncrementalPath (cannot send incrementally)
	doTest(l("@a"), l("#a", "#b", "@c"), func(d FilesystemDiff) {
		assert.Equal(t, l("#a", "@c"), d.IncrementalPath)
	})

	// test that snapshots are preferred over bookmarks in IncrementalPath
	doTest(l("@a"), l("#a", "@a", "@b"), func(d FilesystemDiff) {
		assert.Equal(t, l("@a", "@b"), d.IncrementalPath)
	})
	doTest(l("@a"), l("@a", "#a", "@b"), func(d FilesystemDiff) {
		assert.Equal(t, l("@a", "@b"), d.IncrementalPath)
	})

}
