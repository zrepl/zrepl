package tests

import (
	"path"

	"github.com/stretchr/testify/require"

	"github.com/LyingCak3/zrepl/internal/platformtest"
	"github.com/LyingCak3/zrepl/internal/zfs"
)

func HoldsWork(ctx *platformtest.Context) {
	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "foo bar"
		+  "foo bar@snap name"
	`)

	fs := path.Join(ctx.RootDataset, "foo bar")

	err := zfs.ZFSHold(ctx, fs, fsversion(ctx, fs, "@snap name"), "tag 1")
	require.NoError(ctx, err)

	err = zfs.ZFSHold(ctx, fs, fsversion(ctx, fs, "@snap name"), "tag 2")
	require.NoError(ctx, err)

	holds, err := zfs.ZFSHolds(ctx, fs, "snap name")
	require.NoError(ctx, err)
	require.Len(ctx, holds, 2)
	require.Contains(ctx, holds, "tag 1")
	require.Contains(ctx, holds, "tag 2")
}
