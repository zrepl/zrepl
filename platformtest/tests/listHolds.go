package tests

import (
	"fmt"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func ListHolds(ctx *platformtest.Context) {

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
	v1 := sendArgVersion(fs, "@1")

	tag := "zrepl_platformtest"
	err := zfs.ZFSHold(ctx, fs, v1, tag)
	if err != nil {
		panic(err)
	}

	err = zfs.ZFSHold(ctx, fs, v1, tag)
	if err != nil {
		panic(err)
	}

	vnonexistent := zfs.ZFSSendArgVersion{
		RelName: "@nonexistent",
		GUID:    0xbadf00d,
	}
	err = zfs.ZFSHold(ctx, fs, vnonexistent, tag)
	if err == nil {
		panic("still expecting error for nonexistent snapshot")
	}

}
