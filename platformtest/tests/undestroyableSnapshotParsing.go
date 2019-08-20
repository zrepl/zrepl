package tests

import (
	"fmt"

	"github.com/stretchr/testify/require"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func UndestroyableSnapshotParsing(t *platformtest.Context) {
	platformtest.Run(t, platformtest.PanicErr, t.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "foo bar"
		+  "foo bar@1 2 3"
		+  "foo bar@4 5 6"
		+  "foo bar@7 8 9"
		R  zfs hold zrepl_platformtest "${ROOTDS}/foo bar@4 5 6"
	`)
	defer platformtest.Run(t, platformtest.PanicErr, t.RootDataset, `
	R zfs release zrepl_platformtest "${ROOTDS}/foo bar@4 5 6"
	DESTROYROOT
	`)

	err := zfs.ZFSDestroy(fmt.Sprintf("%s/foo bar@1 2 3,4 5 6,7 8 9", t.RootDataset))
	if err == nil {
		panic("expecting destroy error due to hold")
	}
	if dse, ok := err.(*zfs.DestroySnapshotsError); !ok {
		panic(fmt.Sprintf("expecting *zfs.DestroySnapshotsError, got %T\n%v\n%s", err, err, err))
	} else {
		if dse.Filesystem != fmt.Sprintf("%s/foo bar", t.RootDataset) {
			panic(dse.Filesystem)
		}
		require.Equal(t, []string{"4 5 6"}, dse.Undestroyable)
		require.Equal(t, []string{"dataset is busy"}, dse.Reason)
	}

}
