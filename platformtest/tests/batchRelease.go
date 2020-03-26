package tests

import (
	"fmt"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

type rollupReleaseExpectTags struct {
	Snap  string
	Holds map[string]bool
}

func rollupReleaseTest(ctx *platformtest.Context, cb func(fs string) []rollupReleaseExpectTags) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
	DESTROYROOT
	CREATEROOT
	+  "foo bar"
	+  "foo bar@1"
	+  "foo bar@2"
	+  "foo bar@3"
	+  "foo bar@4"
	+  "foo bar@5"
	+  "foo bar@6"
	R  zfs hold zrepl_platformtest   "${ROOTDS}/foo bar@1"
	R  zfs hold zrepl_platformtest_2 "${ROOTDS}/foo bar@2"
	R  zfs hold zrepl_platformtest   "${ROOTDS}/foo bar@3"
	R  zfs hold zrepl_platformtest   "${ROOTDS}/foo bar@5"
	R  zfs hold zrepl_platformtest   "${ROOTDS}/foo bar@6"
	R  zfs bookmark "${ROOTDS}/foo bar@5" "${ROOTDS}/foo bar#5"
`)

	fs := fmt.Sprintf("%s/foo bar", ctx.RootDataset)

	expTags := cb(fs)

	for _, exp := range expTags {
		holds, err := zfs.ZFSHolds(ctx, fs, exp.Snap)
		if err != nil {
			panic(err)
		}
		for _, h := range holds {
			if e, ok := exp.Holds[h]; !ok || !e {
				panic(fmt.Sprintf("tag %q on snap %q not expected", h, exp.Snap))
			}
		}
	}

}

func RollupReleaseIncluding(ctx *platformtest.Context) {
	rollupReleaseTest(ctx, func(fs string) []rollupReleaseExpectTags {
		guid5, err := zfs.ZFSGetGUID(ctx, fs, "@5")
		require.NoError(ctx, err)

		err = zfs.ZFSReleaseAllOlderAndIncludingGUID(ctx, fs, guid5, "zrepl_platformtest")
		require.NoError(ctx, err)

		return []rollupReleaseExpectTags{
			{"1", map[string]bool{}},
			{"2", map[string]bool{"zrepl_platformtest_2": true}},
			{"3", map[string]bool{}},
			{"4", map[string]bool{}},
			{"5", map[string]bool{}},
			{"6", map[string]bool{"zrepl_platformtest": true}},
		}
	})
}

func RollupReleaseExcluding(ctx *platformtest.Context) {
	rollupReleaseTest(ctx, func(fs string) []rollupReleaseExpectTags {
		guid5, err := zfs.ZFSGetGUID(ctx, fs, "@5")
		require.NoError(ctx, err)

		err = zfs.ZFSReleaseAllOlderThanGUID(ctx, fs, guid5, "zrepl_platformtest")
		require.NoError(ctx, err)

		return []rollupReleaseExpectTags{
			{"1", map[string]bool{}},
			{"2", map[string]bool{"zrepl_platformtest_2": true}},
			{"3", map[string]bool{}},
			{"4", map[string]bool{}},
			{"5", map[string]bool{"zrepl_platformtest": true}},
			{"6", map[string]bool{"zrepl_platformtest": true}},
		}
	})
}

func RollupReleaseMostRecentIsBookmarkWithoutSnapshot(ctx *platformtest.Context) {
	rollupReleaseTest(ctx, func(fs string) []rollupReleaseExpectTags {
		guid5, err := zfs.ZFSGetGUID(ctx, fs, "#5")
		require.NoError(ctx, err)

		err = zfs.ZFSRelease(ctx, "zrepl_platformtest", fs+"@5")
		require.NoError(ctx, err)

		err = zfs.ZFSDestroy(ctx, fs+"@5")
		require.NoError(ctx, err)

		err = zfs.ZFSReleaseAllOlderAndIncludingGUID(ctx, fs, guid5, "zrepl_platformtest")
		require.NoError(ctx, err)

		return []rollupReleaseExpectTags{
			{"1", map[string]bool{}},
			{"2", map[string]bool{"zrepl_platformtest_2": true}},
			{"3", map[string]bool{}},
			{"4", map[string]bool{}},
			// {"5", map[string]bool{}}, doesn't exist
			{"6", map[string]bool{"zrepl_platformtest": true}},
		}
	})
}

func RollupReleaseMostRecentIsBookmarkAndSnapshotStillExists(ctx *platformtest.Context) {
	rollupReleaseTest(ctx, func(fs string) []rollupReleaseExpectTags {
		guid5, err := zfs.ZFSGetGUID(ctx, fs, "#5")
		require.NoError(ctx, err)

		err = zfs.ZFSReleaseAllOlderAndIncludingGUID(ctx, fs, guid5, "zrepl_platformtest")
		require.NoError(ctx, err)

		return []rollupReleaseExpectTags{
			{"1", map[string]bool{}},
			{"2", map[string]bool{"zrepl_platformtest_2": true}},
			{"3", map[string]bool{}},
			{"4", map[string]bool{}},
			{"5", map[string]bool{}},
			{"6", map[string]bool{"zrepl_platformtest": true}},
		}
	})
}

func RollupReleaseMostRecentDoesntExist(ctx *platformtest.Context) {
	rollupReleaseTest(ctx, func(fs string) []rollupReleaseExpectTags {

		const nonexistentGuid = 0 // let's take our chances...
		err := zfs.ZFSReleaseAllOlderAndIncludingGUID(ctx, fs, nonexistentGuid, "zrepl_platformtest")
		require.Error(ctx, err)
		require.Contains(ctx, err.Error(), "cannot find snapshot or bookmark with guid 0")

		return []rollupReleaseExpectTags{
			{"1", map[string]bool{"zrepl_platformtest": true}},
			{"2", map[string]bool{"zrepl_platformtest_2": true}},
			{"3", map[string]bool{"zrepl_platformtest": true}},
			{"4", map[string]bool{"zrepl_platformtest": true}},
			{"5", map[string]bool{"zrepl_platformtest": true}},
			{"6", map[string]bool{"zrepl_platformtest": true}},
		}
	})
}
