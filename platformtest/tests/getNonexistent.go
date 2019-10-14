package tests

import (
	"fmt"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func GetNonexistent(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "foo bar"
		+  "foo bar@1"
	`)

	// test raw
	_, err := zfs.ZFSGetRawAnySource(fmt.Sprintf("%s/foo bar", ctx.RootDataset), []string{"name"})
	if err != nil {
		panic(err)
	}

	// test nonexistent filesystem
	nonexistent := fmt.Sprintf("%s/nonexistent filesystem", ctx.RootDataset)
	props, err := zfs.ZFSGetRawAnySource(nonexistent, []string{"name"})
	if err == nil {
		panic(props)
	}
	dsne, ok := err.(*zfs.DatasetDoesNotExist)
	if !ok {
		panic(err)
	} else if dsne.Path != nonexistent {
		panic(err)
	}

	// test nonexistent snapshot
	nonexistent = fmt.Sprintf("%s/foo bar@non existent", ctx.RootDataset)
	props, err = zfs.ZFSGetRawAnySource(nonexistent, []string{"name"})
	if err == nil {
		panic(props)
	}
	dsne, ok = err.(*zfs.DatasetDoesNotExist)
	if !ok {
		panic(err)
	} else if dsne.Path != nonexistent {
		panic(err)
	}

	// test nonexistent bookmark
	nonexistent = fmt.Sprintf("%s/foo bar#non existent", ctx.RootDataset)
	props, err = zfs.ZFSGetRawAnySource(nonexistent, []string{"name"})
	if err == nil {
		panic(props)
	}
	dsne, ok = err.(*zfs.DatasetDoesNotExist)
	if !ok {
		panic(err)
	} else if dsne.Path != nonexistent {
		panic(err)
	}

}
