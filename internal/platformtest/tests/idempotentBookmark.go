package tests

import (
	"fmt"

	"github.com/stretchr/testify/assert"

	"github.com/zrepl/zrepl/internal/platformtest"
	"github.com/zrepl/zrepl/internal/zfs"
)

func IdempotentBookmark(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "foo bar"
		+  "foo bar@a snap"
		+  "foo bar@another snap"
	`)

	fs := fmt.Sprintf("%s/foo bar", ctx.RootDataset)

	asnap := fsversion(ctx, fs, "@a snap")
	anotherSnap := fsversion(ctx, fs, "@another snap")

	aBookmark, err := zfs.ZFSBookmark(ctx, fs, asnap, "a bookmark")
	if err != nil {
		panic(err)
	}

	// do it again, should be idempotent
	aBookmarkIdemp, err := zfs.ZFSBookmark(ctx, fs, asnap, "a bookmark")
	if err != nil {
		panic(err)
	}
	assert.Equal(ctx, aBookmark, aBookmarkIdemp)

	// should fail for another snapshot
	_, err = zfs.ZFSBookmark(ctx, fs, anotherSnap, "a bookmark")
	if err == nil {
		panic(err)
	}
	if _, ok := err.(*zfs.BookmarkExists); !ok {
		panic(fmt.Sprintf("has type %T", err))
	}

	// destroy the snapshot
	if err := zfs.ZFSDestroy(ctx, fmt.Sprintf("%s@a snap", fs)); err != nil {
		panic(err)
	}

	// do it again, should fail with special error type
	_, err = zfs.ZFSBookmark(ctx, fs, asnap, "a bookmark")
	if err == nil {
		panic(err)
	}
	if _, ok := err.(*zfs.DatasetDoesNotExist); !ok {
		panic(fmt.Sprintf("has type %T", err))
	}

}
