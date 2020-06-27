package tests

import (
	"fmt"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func CreateReplicationCursor(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "foo bar"
		+  "foo bar@1 with space"
		R  zfs bookmark "${ROOTDS}/foo bar@1 with space" "${ROOTDS}/foo bar#1 with space"
		+  "foo bar@2 with space"
		R  zfs bookmark "${ROOTDS}/foo bar@2 with space" "${ROOTDS}/foo bar#2 with space"
		+  "foo bar@3 with space"
		R  zfs bookmark "${ROOTDS}/foo bar@3 with space" "${ROOTDS}/foo bar#3 with space"
		-  "foo bar@3 with space"
	`)

	jobid := endpoint.MustMakeJobID("zreplplatformtest")

	ds, err := zfs.NewDatasetPath(ctx.RootDataset + "/foo bar")
	if err != nil {
		panic(err)
	}

	fs := ds.ToString()

	checkCreateCursor := func(createErr error, c endpoint.Abstraction, references zfs.FilesystemVersion) {
		assert.NoError(ctx, createErr)
		expectName, err := endpoint.ReplicationCursorBookmarkName(fs, references.Guid, jobid)
		assert.NoError(ctx, err)
		require.Equal(ctx, expectName, c.GetFilesystemVersion().Name)
	}

	snap := fsversion(ctx, fs, "@1 with space")
	book := fsversion(ctx, fs, "#1 with space")
	book3 := fsversion(ctx, fs, "#3 with space")

	// create first cursor
	cursorOfSnap, err := endpoint.CreateReplicationCursor(ctx, fs, snap, jobid)
	checkCreateCursor(err, cursorOfSnap, snap)
	// check CreateReplicationCursor is idempotent (for snapshot target)
	cursorOfSnapIdemp, err := endpoint.CreateReplicationCursor(ctx, fs, snap, jobid)
	checkCreateCursor(err, cursorOfSnap, snap)
	// check CreateReplicationCursor is idempotent (for bookmark target of snapshot)
	cursorOfBook, err := endpoint.CreateReplicationCursor(ctx, fs, book, jobid)
	checkCreateCursor(err, cursorOfBook, snap)
	// ... for target = non-cursor bookmark
	_, err = endpoint.CreateReplicationCursor(ctx, fs, book3, jobid)
	assert.Equal(ctx, zfs.ErrBookmarkCloningNotSupported, err)
	// ... for target = replication cursor bookmark to be created
	cursorOfCursor, err := endpoint.CreateReplicationCursor(ctx, fs, cursorOfSnapIdemp.GetFilesystemVersion(), jobid)
	checkCreateCursor(err, cursorOfCursor, cursorOfSnap.GetFilesystemVersion())

	snapProps, err := zfs.ZFSGetFilesystemVersion(ctx, snap.FullPath(fs))
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

}
