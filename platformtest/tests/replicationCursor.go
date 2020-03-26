package tests

import (
	"fmt"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func ReplicationCursor(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "foo bar"
		+  "foo bar@1 with space"
		R  zfs bookmark "${ROOTDS}/foo bar@1 with space" "${ROOTDS}/foo bar#1 with space"
		+  "foo bar@2 with space"
		R  zfs bookmark "${ROOTDS}/foo bar@1 with space" "${ROOTDS}/foo bar#2 with space"
		+  "foo bar@3 with space"
		R  zfs bookmark "${ROOTDS}/foo bar@1 with space" "${ROOTDS}/foo bar#3 with space"
	`)

	jobid := endpoint.MustMakeJobID("zreplplatformtest")

	ds, err := zfs.NewDatasetPath(ctx.RootDataset + "/foo bar")
	if err != nil {
		panic(err)
	}

	fs := ds.ToString()
	snap := fsversion(fs, "@1 with space")

	destroyed, err := endpoint.MoveReplicationCursor(ctx, fs, &snap, jobid)
	if err != nil {
		panic(err)
	}
	assert.Empty(ctx, destroyed)

	snapProps, err := zfs.ZFSGetFilesystemVersion(snap.FullPath(fs))
	if err != nil {
		panic(err)
	}

	bm, err := endpoint.GetMostRecentReplicationCursorOfJob(ctx, fs, jobid)
	if err != nil {
		panic(err)
	}
	if bm.CreateTXG != snapProps.CreateTXG {
		panic(fmt.Sprintf("createtxgs do not match: %v != %v", bm.CreateTXG, snapProps.CreateTXG))
	}
	if bm.Guid != snapProps.Guid {
		panic(fmt.Sprintf("guids do not match: %v != %v", bm.Guid, snapProps.Guid))
	}

	// try moving
	cursor1BookmarkName, err := endpoint.ReplicationCursorBookmarkName(fs, snap.Guid, jobid)
	require.NoError(ctx, err)

	snap2 := fsversion(fs, "@2 with space")
	destroyed, err = endpoint.MoveReplicationCursor(ctx, fs, &snap2, jobid)
	require.NoError(ctx, err)
	require.Equal(ctx, 1, len(destroyed))
	require.Equal(ctx, endpoint.AbstractionReplicationCursorBookmarkV2, destroyed[0].GetType())
	require.Equal(ctx, cursor1BookmarkName, destroyed[0].GetName())
}
