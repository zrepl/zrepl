package tests

import (
	"fmt"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func ReplicationCursor(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "foo bar"
		+  "foo bar@1 with space"
	`)

	ds, err := zfs.NewDatasetPath(ctx.RootDataset + "/foo bar")
	if err != nil {
		panic(err)
	}
	guid, err := zfs.ZFSSetReplicationCursor(ds, "1 with space")
	if err != nil {
		panic(err)
	}
	snapProps, err := zfs.ZFSGetCreateTXGAndGuid(ds.ToString() + "@1 with space")
	if err != nil {
		panic(err)
	}
	if guid != snapProps.Guid {
		panic(fmt.Sprintf("guids to not match: %v != %v", guid, snapProps.Guid))
	}

	bm, err := zfs.ZFSGetReplicationCursor(ds)
	if err != nil {
		panic(err)
	}
	if bm.CreateTXG != snapProps.CreateTXG {
		panic(fmt.Sprintf("createtxgs do not match: %v != %v", bm.CreateTXG, snapProps.CreateTXG))
	}
	if bm.Guid != snapProps.Guid {
		panic(fmt.Sprintf("guids do not match: %v != %v", bm.Guid, snapProps.Guid))
	}
	if bm.Guid != guid {
		panic(fmt.Sprintf("guids do not match: %v != %v", bm.Guid, guid))
	}

	// test nonexistent
	err = zfs.ZFSDestroyFilesystemVersion(ds, bm)
	if err != nil {
		panic(err)
	}
	bm2, err := zfs.ZFSGetReplicationCursor(ds)
	if bm2 != nil {
		panic(fmt.Sprintf("expecting no replication cursor after deleting it, got %v", bm))
	}
	if err != nil {
		panic(fmt.Sprintf("expecting no error for getting nonexistent replication cursor, bot %v", err))
	}

	// TODO test moving the replication cursor
}
