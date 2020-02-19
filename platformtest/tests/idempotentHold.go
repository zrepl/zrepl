package tests

import (
	"fmt"

	"github.com/stretchr/testify/require"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func IdempotentHold(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "foo bar"
		+  "foo bar@1 2"
	`)

	fs := fmt.Sprintf("%s/foo bar", ctx.RootDataset)
	snap := sendArgVersion(fs, "@1 2")

	tag := "zrepl_platformtest"
	err := zfs.ZFSHold(ctx, fs, snap, tag)
	if err != nil {
		panic(err)
	}

	// existing holds
	holds, err := zfs.ZFSHolds(ctx, fs, "1 2")
	require.NoError(ctx, err)
	require.Equal(ctx, []string{tag}, holds)

	holds, err = zfs.ZFSHolds(ctx, fs, "non existent")
	ctx.Logf("holds=%v", holds)
	ctx.Logf("errT=%T", err)
	ctx.Logf("err=%s", err)
	notExist, ok := err.(*zfs.DatasetDoesNotExist)
	require.True(ctx, ok)
	require.Equal(ctx, fs+"@non existent", notExist.Path)
}
