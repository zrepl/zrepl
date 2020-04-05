package tests

import (
	"strings"

	"github.com/stretchr/testify/require"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func ListFilesystemsNoFilter(t *platformtest.Context) {
	platformtest.Run(t, platformtest.PanicErr, t.RootDataset, `
		DESTROYROOT
		CREATEROOT
		R  zfs create -V 10M "${ROOTDS}/bar baz"
		+  "foo bar"
		+  "foo bar/bar blup"
		+  "foo bar/blah"
		R  zfs create -V 10M "${ROOTDS}/foo bar/blah/a volume"
	`)

	fss, err := zfs.ZFSListMapping(t, zfs.NoFilter())
	require.NoError(t, err)
	var onlyTestPool []*zfs.DatasetPath
	for _, fs := range fss {
		if strings.HasPrefix(fs.ToString(), t.RootDataset) {
			onlyTestPool = append(onlyTestPool, fs)
		}
	}
	onlyTestPoolStr := datasetToStringSortedTrimPrefix(mustDatasetPath(t.RootDataset), onlyTestPool)
	require.Equal(t, []string{"bar baz", "foo bar", "foo bar/bar blup", "foo bar/blah", "foo bar/blah/a volume"}, onlyTestPoolStr)

}
