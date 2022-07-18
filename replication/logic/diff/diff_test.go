package diff

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/zrepl/zrepl/replication/logic/pdu"
)

func fsvlist(fsv ...string) (r []*FilesystemVersion) {

	r = make([]*FilesystemVersion, len(fsv))
	for i, f := range fsv {

		// parse the id from fsvlist. it is used to derive Guid,CreateTXG and Creation attrs
		split := strings.Split(f, ",")
		if len(split) != 2 {
			panic("invalid fsv spec")
		}
		id, err := strconv.Atoi(split[1])
		if err != nil {
			panic(err)
		}
		creation := func(id int) string {
			return FilesystemVersionCreation(time.Unix(0, 0).Add(time.Duration(id) * time.Second))
		}
		if strings.HasPrefix(f, "#") {
			r[i] = &FilesystemVersion{
				Name:      strings.TrimPrefix(f, "#"),
				Type:      FilesystemVersion_Bookmark,
				Guid:      uint64(id),
				CreateTXG: uint64(id),
				Creation:  creation(id),
			}
		} else if strings.HasPrefix(f, "@") {
			r[i] = &FilesystemVersion{
				Name:      strings.TrimPrefix(f, "@"),
				Type:      FilesystemVersion_Snapshot,
				Guid:      uint64(id),
				CreateTXG: uint64(id),
				Creation:  creation(id),
			}
		} else {
			panic("invalid character")
		}
	}
	return
}

func doTest(receiver, sender []*FilesystemVersion, validate func(incpath []*FilesystemVersion, conflict error)) {
	p, err := IncrementalPath(receiver, sender)
	validate(p, err)
}

func TestIncrementalPath_SnapshotsOnly(t *testing.T) {

	l := fsvlist

	// basic functionality
	doTest(l("@a,1", "@b,2"), l("@a,1", "@b,2", "@c,3", "@d,4"), func(path []*FilesystemVersion, conflict error) {
		assert.Equal(t, l("@b,2", "@c,3", "@d,4"), path)
	})

	// no common ancestor
	doTest(l(), l("@a,1"), func(path []*FilesystemVersion, conflict error) {
		assert.Nil(t, path)
		ca, ok := conflict.(*ConflictNoCommonAncestor)
		require.True(t, ok)
		assert.Equal(t, l("@a,1"), ca.SortedSenderVersions)
	})
	doTest(l("@a,1", "@b,2"), l("@c,3", "@d,4"), func(path []*FilesystemVersion, conflict error) {
		assert.Nil(t, path)
		ca, ok := conflict.(*ConflictNoCommonAncestor)
		require.True(t, ok)
		assert.Equal(t, l("@a,1", "@b,2"), ca.SortedReceiverVersions)
		assert.Equal(t, l("@c,3", "@d,4"), ca.SortedSenderVersions)
	})

	// divergence is detected
	doTest(l("@a,1", "@b1,2"), l("@a,1", "@b2,3"), func(path []*FilesystemVersion, conflict error) {
		assert.Nil(t, path)
		cd, ok := conflict.(*ConflictDiverged)
		require.True(t, ok)
		assert.Equal(t, l("@a,1")[0], cd.CommonAncestor)
		assert.Equal(t, l("@b1,2"), cd.ReceiverOnly)
		assert.Equal(t, l("@b2,3"), cd.SenderOnly)
	})

	// gaps before most recent common ancestor do not matter
	doTest(l("@a,1", "@b,2", "@c,3"), l("@a,1", "@c,3", "@d,4"), func(path []*FilesystemVersion, conflict error) {
		assert.Equal(t, l("@c,3", "@d,4"), path)
	})

	// nothing to do if fully shared history
	doTest(l("@a,1", "@b,2"), l("@a,1", "@b,2"), func(incpath []*FilesystemVersion, conflict error) {
		assert.Nil(t, incpath)
		assert.NotNil(t, conflict)
		_, ok := conflict.(*ConflictMostRecentSnapshotAlreadyPresent)
		assert.True(t, ok)
	})

	// ...but it's sufficient if the most recent snapshot is present
	doTest(l("@c,3"), l("@a,1", "@b,2", "@c,3"), func(path []*FilesystemVersion, conflict error) {
		assert.Nil(t, path)
		_, ok := conflict.(*ConflictMostRecentSnapshotAlreadyPresent)
		assert.True(t, ok)
	})

	// no sender snapshots errors: empty receiver
	doTest(l(), l(), func(incpath []*FilesystemVersion, conflict error) {
		assert.Nil(t, incpath)
		assert.NotNil(t, conflict)
		t.Logf("%T", conflict)
		_, ok := conflict.(*ConflictNoSenderSnapshots)
		assert.True(t, ok)
	})

	// no sender snapshots errors: snapshots on receiver
	doTest(l("@a,1"), l(), func(incpath []*FilesystemVersion, conflict error) {
		assert.Nil(t, incpath)
		assert.NotNil(t, conflict)
		t.Logf("%T", conflict)
		_, ok := conflict.(*ConflictNoSenderSnapshots)
		assert.True(t, ok)
	})

}

func TestIncrementalPath_BookmarkSupport(t *testing.T) {
	l := fsvlist

	// bookmarks are used
	doTest(l("@a,1"), l("#a,1", "@b,2"), func(path []*FilesystemVersion, conflict error) {
		assert.Equal(t, l("#a,1", "@b,2"), path)
	})

	// bookmarks are stripped from IncrementalPath (cannot send incrementally)
	doTest(l("@a,1"), l("#a,1", "#b,2", "@c,3"), func(path []*FilesystemVersion, conflict error) {
		assert.Equal(t, l("#a,1", "@c,3"), path)
	})

	// test that snapshots are preferred over bookmarks in IncrementalPath
	doTest(l("@a,1"), l("#a,1", "@a,1", "@b,2"), func(path []*FilesystemVersion, conflict error) {
		assert.Equal(t, l("@a,1", "@b,2"), path)
	})
	doTest(l("@a,1"), l("@a,1", "#a,1", "@b,2"), func(path []*FilesystemVersion, conflict error) {
		assert.Equal(t, l("@a,1", "@b,2"), path)
	})

	// test that receive-side bookmarks younger than the most recent common ancestor do not cause a conflict
	doTest(l("@a,1", "#b,2"), l("@a,1", "@c,3"), func(path []*FilesystemVersion, conflict error) {
		assert.NoError(t, conflict)
		require.Len(t, path, 2)
		assert.Equal(t, l("@a,1")[0], path[0])
		assert.Equal(t, l("@c,3")[0], path[1])
	})
	doTest(l("#a,1"), l("@a,1", "@b,2"), func(path []*FilesystemVersion, conflict error) {
		assert.Nil(t, path)
		ca, ok := conflict.(*ConflictNoCommonAncestor)
		require.True(t, ok)
		assert.Equal(t, l(), ca.SortedReceiverVersions, "See comment in IncrementalPath() on why we don't include the boomkmark here")
	})

}
