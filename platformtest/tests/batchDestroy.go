package tests

import (
	"fmt"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/require"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func BatchDestroy(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "foo bar"
		+  "foo bar@1"
		+  "foo bar@2"
		+  "foo bar@3"
		+  "foo bar@4"
		R  zfs hold zrepl_platformtest "${ROOTDS}/foo bar@2"
		R  zfs hold zrepl_platformtest "${ROOTDS}/foo bar@4"
	`)

	reqs := []*zfs.DestroySnapOp{
		&zfs.DestroySnapOp{
			ErrOut:     new(error),
			Filesystem: fmt.Sprintf("%s/foo bar", ctx.RootDataset),
			Name:       "3",
		},
		&zfs.DestroySnapOp{
			ErrOut:     new(error),
			Filesystem: fmt.Sprintf("%s/foo bar", ctx.RootDataset),
			Name:       "2",
		},
		&zfs.DestroySnapOp{
			ErrOut:     new(error),
			Filesystem: fmt.Sprintf("%s/foo bar", ctx.RootDataset),
			Name:       "non existent",
		},
		&zfs.DestroySnapOp{
			ErrOut:     new(error),
			Filesystem: fmt.Sprintf("%s/foo bar", ctx.RootDataset),
			Name:       "4",
		},
	}
	zfs.ZFSDestroyFilesystemVersions(ctx, reqs)

	pretty.Println(reqs)

	if *reqs[0].ErrOut != nil {
		panic("expecting no error")
	}

	eBusy, ok := (*reqs[1].ErrOut).(*zfs.ErrDestroySnapshotDatasetIsBusy)
	require.True(ctx, ok)
	require.Equal(ctx, reqs[1].Name, eBusy.Name)

	require.Nil(ctx, *reqs[2].ErrOut, "destroying non-existent snap is not an error (idempotence)")

	eBusy, ok = (*reqs[3].ErrOut).(*zfs.ErrDestroySnapshotDatasetIsBusy)
	require.True(ctx, ok)
	require.Equal(ctx, reqs[3].Name, eBusy.Name)

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
	!N "foo bar@3"
	!E "foo bar@1"
	!E "foo bar@2"
	!E "foo bar@4"
	`)

}
