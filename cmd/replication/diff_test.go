package replication_test

import (
	"github.com/stretchr/testify/assert"
	"github.com/zrepl/zrepl/cmd/replication"
	"github.com/zrepl/zrepl/zfs"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fsvlist(fsv ...string) (r []zfs.FilesystemVersion) {

	r = make([]zfs.FilesystemVersion, len(fsv))
	for i, f := range fsv {

		// parse the id from fsvlist. it is used to derivce Guid,CreateTXG and Creation attrs
		split := strings.Split(f, ",")
		if len(split) != 2 {
			panic("invalid fsv spec")
		}
		id, err := strconv.Atoi(split[1])
		if err != nil {
			panic(err)
		}

		if strings.HasPrefix(f, "#") {
			r[i] = zfs.FilesystemVersion{
				Name:      strings.TrimPrefix(f, "#"),
				Type:      zfs.Bookmark,
				Guid:      uint64(id),
				CreateTXG: uint64(id),
				Creation:  time.Unix(0, 0).Add(time.Duration(id) * time.Second),
			}
		} else if strings.HasPrefix(f, "@") {
			r[i] = zfs.FilesystemVersion{
				Name:      strings.TrimPrefix(f, "@"),
				Type:      zfs.Snapshot,
				Guid:      uint64(id),
				CreateTXG: uint64(id),
				Creation:  time.Unix(0, 0).Add(time.Duration(id) * time.Second),
			}
		} else {
			panic("invalid character")
		}
	}
	return
}

type incPathResult struct {
	incPath  []zfs.FilesystemVersion
	conflict error
}

type IncrementalPathTest struct {
	Msg                    string
	Receiver, Sender       []zfs.FilesystemVersion
	ExpectIncPath          []zfs.FilesystemVersion
	ExpectNoCommonAncestor bool
	ExpectDiverged         *replication.ConflictDiverged
	ExpectPanic            bool
}

func (tt *IncrementalPathTest) Test(t *testing.T) {

	t.Logf("test: %s", tt.Msg)

	if tt.ExpectPanic {
		assert.Panics(t, func() {
			replication.IncrementalPath(tt.Receiver, tt.Sender)
		})
		return
	}

	incPath, conflict := replication.IncrementalPath(tt.Receiver, tt.Sender)

	if tt.ExpectIncPath != nil {
		assert.Nil(t, conflict)
		assert.True(t, len(incPath) == 0 || len(incPath) >= 2)
		assert.Equal(t, tt.ExpectIncPath, incPath)
		return
	}
	if conflict == nil {
		t.Logf("conflict is (unexpectly) <nil>\nincPath: %#v", incPath)
	}
	if tt.ExpectNoCommonAncestor {
		assert.IsType(t, &replication.ConflictNoCommonAncestor{}, conflict)
		// TODO check sorting
		return
	}
	if tt.ExpectDiverged != nil {
		if !assert.IsType(t, &replication.ConflictDiverged{}, conflict) {
			return
		}
		c := conflict.(*replication.ConflictDiverged)
		// TODO check sorting
		assert.NotZero(t, c.CommonAncestor)
		assert.NotEmpty(t, c.ReceiverOnly)
		assert.Equal(t, tt.ExpectDiverged.ReceiverOnly, c.ReceiverOnly)
		assert.Equal(t, tt.ExpectDiverged.SenderOnly, c.SenderOnly)
		return
	}

}

func TestIncrementalPlan_IncrementalSnapshots(t *testing.T) {
	l := fsvlist

	tbl := []IncrementalPathTest{
		{
			Msg:           "basic functionality",
			Receiver:      l("@a,1", "@b,2"),
			Sender:        l("@a,1", "@b,2", "@c,3", "@d,4"),
			ExpectIncPath: l("@b,2", "@c,3", "@d,4"),
		},
		{
			Msg:                    "no snaps on receiver yields no common ancestor",
			Receiver:               l(),
			Sender:                 l("@a,1"),
			ExpectNoCommonAncestor: true,
		},
		{
			Msg:           "no snapshots on sender yields empty incremental path",
			Receiver:      l(),
			Sender:        l(),
			ExpectIncPath: l(),
		},
		{
			Msg:           "nothing to do yields empty incremental path",
			Receiver:      l("@a,1"),
			Sender:        l("@a,1"),
			ExpectIncPath: l(),
		},
		{
			Msg:                    "drifting apart",
			Receiver:               l("@a,1", "@b,2"),
			Sender:                 l("@c,3", "@d,4"),
			ExpectNoCommonAncestor: true,
		},
		{
			Msg:      "different snapshots on sender and receiver",
			Receiver: l("@a,1", "@c,2"),
			Sender:   l("@a,1", "@b,3"),
			ExpectDiverged: &replication.ConflictDiverged{
				CommonAncestor: l("@a,1")[0],
				SenderOnly:     l("@b,3"),
				ReceiverOnly:   l("@c,2"),
			},
		},
		{
			Msg:      "snapshot on receiver not present on sender",
			Receiver: l("@a,1", "@b,2"),
			Sender:   l("@a,1"),
			ExpectDiverged: &replication.ConflictDiverged{
				CommonAncestor: l("@a,1")[0],
				SenderOnly:     l(),
				ReceiverOnly:   l("@b,2"),
			},
		},
		{
			Msg:           "gaps before most recent common ancestor do not matter",
			Receiver:      l("@a,1", "@b,2", "@c,3"),
			Sender:        l("@a,1", "@c,3", "@d,4"),
			ExpectIncPath: l("@c,3", "@d,4"),
		},
	}

	for _, test := range tbl {
		test.Test(t)
	}

}

func TestIncrementalPlan_BookmarksSupport(t *testing.T) {
	l := fsvlist

	tbl := []IncrementalPathTest{
		{
			Msg:           "bookmarks are used",
			Receiver:      l("@a,1"),
			Sender:        l("#a,1", "@b,2"),
			ExpectIncPath: l("#a,1", "@b,2"),
		},
		{
			Msg:           "boomarks are stripped from incPath (cannot send incrementally)",
			Receiver:      l("@a,1"),
			Sender:        l("#a,1", "#b,2", "@c,3"),
			ExpectIncPath: l("#a,1", "@c,3"),
		},
		{
			Msg:           "bookmarks are preferred over snapshots for start of incPath",
			Receiver:      l("@a,1"),
			Sender:        l("#a,1", "@a,1", "@b,2"),
			ExpectIncPath: l("#a,1", "@b,2"),
		},
		{
			Msg:           "bookmarks are preferred over snapshots for start of incPath (regardless of order)",
			Receiver:      l("@a,1"),
			Sender:        l("@a,1", "#a,1", "@b,2"),
			ExpectIncPath: l("#a,1", "@b,2"),
		},
	}

	for _, test := range tbl {
		test.Test(t)
	}

}

func TestSortVersionListByCreateTXGThenBookmarkLTSnapshot(t *testing.T) {

	type Test struct {
		Msg           string
		Input, Output []zfs.FilesystemVersion
	}

	l := fsvlist

	tbl := []Test{
		{
			"snapshot sorting already sorted",
			l("@a,1", "@b,2"),
			l("@a,1", "@b,2"),
		},
		{
			"bookmark sorting already sorted",
			l("#a,1", "#b,2"),
			l("#a,1", "#b,2"),
		},
		{
			"snapshot sorting",
			l("@b,2", "@a,1"),
			l("@a,1", "@b,2"),
		},
		{
			"bookmark sorting",
			l("#b,2", "#a,1"),
			l("#a,1", "#b,2"),
		},
	}

	for _, test := range tbl {
		t.Logf("test: %s", test.Msg)
		inputlen := len(test.Input)
		sorted := replication.SortVersionListByCreateTXGThenBookmarkLTSnapshot(test.Input)
		if len(sorted) != inputlen {
			t.Errorf("lenghts of input and output do not match: %d vs %d", inputlen, len(sorted))
			continue
		}
		if !assert.Equal(t, test.Output, sorted) {
			continue
		}
		last := sorted[0]
		for _, s := range sorted[1:] {
			if s.CreateTXG < last.CreateTXG {
				t.Errorf("must be sorted ascending, got:\n\t%#v", sorted)
				break
			}
			if s.CreateTXG == last.CreateTXG {
				if last.Type == zfs.Bookmark && s.Type != zfs.Snapshot {
					t.Errorf("snapshots must come after bookmarks")
				}
			}
			last = s
		}
	}

}
