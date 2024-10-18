package tests

import (
	"fmt"

	"github.com/zrepl/zrepl/internal/platformtest"
	"github.com/zrepl/zrepl/internal/zfs"
)

func IdempotentHold(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "foo bar"
		+  "foo bar@1"
	`)
	defer platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		R zfs release zrepl_platformtest "${ROOTDS}/foo bar@1"
		-  "foo bar@1"
		-  "foo bar"
	`)

	fs := fmt.Sprintf("%s/foo bar", ctx.RootDataset)
	v1 := fsversion(ctx, fs, "@1")

	tag := "zrepl_platformtest"
	err := zfs.ZFSHold(ctx, fs, v1, tag)
	if err != nil {
		panic(err)
	}

	err = zfs.ZFSHold(ctx, fs, v1, tag)
	if err != nil {
		panic(err)
	}
}
