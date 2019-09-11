package tests

import (
	"fmt"
	"log"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func IdempotentDestroy(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "foo bar"
		+  "foo bar@a snap"
	`)

	fs := fmt.Sprintf("%s/foo bar", ctx.RootDataset)
	asnap := sendArgVersion(fs, "@a snap")
	err := zfs.ZFSBookmark(fs, asnap, "a bookmark")
	if err != nil {
		panic(err)
	}

	type testCase struct {
		description, path string
	}

	cases := []testCase{
		{"snapshot", fmt.Sprintf("%s@a snap", fs)},
		{"bookmark", fmt.Sprintf("%s#a bookmark", fs)},
		{"filesystem", fs},
	}

	for i := range cases {
		func() {
			c := cases[i]

			log.Printf("SUBBEGIN testing idempotent destroy %q for path %q", c.description, c.path)

			log.Println("destroy existing")
			err = zfs.ZFSDestroy(c.path)
			if err != nil {
				panic(err)
			}
			log.Println("destroy again, non-idempotently, must error")
			err = zfs.ZFSDestroy(c.path)
			if _, ok := err.(*zfs.DatasetDoesNotExist); !ok {
				panic(fmt.Sprintf("%T: %s", err, err))
			}
			log.Println("destroy again, idempotently, must not error")
			err = zfs.ZFSDestroyIdempotent(c.path)
			if err != nil {
				panic(err)
			}

			log.Println("SUBEND")

		}()
	}

	// also test idempotent destroy for cases where the parent dataset does not exist
	err = zfs.ZFSDestroyIdempotent(fmt.Sprintf("%s/not foo bar@nonexistent snapshot", ctx.RootDataset))
	if err != nil {
		panic(err)
	}

	err = zfs.ZFSDestroyIdempotent(fmt.Sprintf("%s/not foo bar#nonexistent bookmark", ctx.RootDataset))
	if err != nil {
		panic(err)
	}

}
