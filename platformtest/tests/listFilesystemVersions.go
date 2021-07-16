package tests

import (
	"fmt"
	"sort"
	"strings"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func ListFilesystemVersionsTypeFilteringAndPrefix(t *platformtest.Context) {
	platformtest.Run(t, platformtest.PanicErr, t.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "foo bar"
		+  "foo bar@foo 1"
		+  "foo bar#foo 1" "foo bar@foo 1"
		+  "foo bar#bookfoo 1" "foo bar@foo 1"
		+  "foo bar@foo 2"
		+  "foo bar#foo 2" "foo bar@foo 2"
		+  "foo bar#bookfoo 2" "foo bar@foo 2"
		+  "foo bar@blup 1"
		+  "foo bar#blup 1" "foo bar@blup 1"
		+  "foo bar@ foo with leading whitespace"

		# repeat the whole thing for a child dataset to make sure we disable recursion

		+  "foo bar/child dataset"
		+  "foo bar/child dataset@foo 1"
		+  "foo bar/child dataset#foo 1" "foo bar/child dataset@foo 1"
		+  "foo bar/child dataset#bookfoo 1" "foo bar/child dataset@foo 1"
		+  "foo bar/child dataset@foo 2"
		+  "foo bar/child dataset#foo 2" "foo bar/child dataset@foo 2"
		+  "foo bar/child dataset#bookfoo 2" "foo bar/child dataset@foo 2"
		+  "foo bar/child dataset@blup 1"
		+  "foo bar/child dataset#blup 1" "foo bar/child dataset@blup 1"
		+  "foo bar/child dataset@ foo with leading whitespace"
	`)

	fs := fmt.Sprintf("%s/foo bar", t.RootDataset)

	// no options := all types
	vs, err := zfs.ZFSListFilesystemVersions(t, mustDatasetPath(fs), zfs.ListFilesystemVersionsOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{
		"#blup 1", "#bookfoo 1", "#bookfoo 2", "#foo 1", "#foo 2",
		"@ foo with leading whitespace", "@blup 1", "@foo 1", "@foo 2",
	}, versionRelnamesSorted(vs))

	// just snapshots
	vs, err = zfs.ZFSListFilesystemVersions(t, mustDatasetPath(fs), zfs.ListFilesystemVersionsOptions{
		Types: zfs.Snapshots,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"@ foo with leading whitespace", "@blup 1", "@foo 1", "@foo 2"}, versionRelnamesSorted(vs))

	// just bookmarks
	vs, err = zfs.ZFSListFilesystemVersions(t, mustDatasetPath(fs), zfs.ListFilesystemVersionsOptions{
		Types: zfs.Bookmarks,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"#blup 1", "#bookfoo 1", "#bookfoo 2", "#foo 1", "#foo 2"}, versionRelnamesSorted(vs))

	// just with prefix foo
	vs, err = zfs.ZFSListFilesystemVersions(t, mustDatasetPath(fs), zfs.ListFilesystemVersionsOptions{
		SnapPrefix: "foo",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"#foo 1", "#foo 2", "@foo 1", "@foo 2"}, versionRelnamesSorted(vs))

}

func ListFilesystemVersionsZeroExistIsNotAnError(t *platformtest.Context) {
	platformtest.Run(t, platformtest.PanicErr, t.RootDataset, `
	DESTROYROOT
	CREATEROOT
	+ "foo bar"
	`)

	fs := fmt.Sprintf("%s/foo bar", t.RootDataset)

	vs, err := zfs.ZFSListFilesystemVersions(t, mustDatasetPath(fs), zfs.ListFilesystemVersionsOptions{})
	require.Empty(t, vs)
	require.NoError(t, err)
}

func ListFilesystemVersionsFilesystemNotExist(t *platformtest.Context) {
	platformtest.Run(t, platformtest.PanicErr, t.RootDataset, `
	DESTROYROOT
	CREATEROOT
	`)

	nonexistentFS := fmt.Sprintf("%s/not existent", t.RootDataset)

	vs, err := zfs.ZFSListFilesystemVersions(t, mustDatasetPath(nonexistentFS), zfs.ListFilesystemVersionsOptions{})
	require.Empty(t, vs)
	require.Error(t, err)
	t.Logf("err = %T\n%s", err, err)
	dsne, ok := err.(*zfs.DatasetDoesNotExist)
	require.True(t, ok)
	require.Equal(t, nonexistentFS, dsne.Path)
}

func ListFilesystemVersionsUserrefs(t *platformtest.Context) {
	platformtest.Run(t, platformtest.PanicErr, t.RootDataset, `
	DESTROYROOT
	CREATEROOT
	+ "foo bar"
	+ "foo bar@snap 1"
	+ "foo bar#snap 1" "foo bar@snap 1"
	+ "foo bar@snap 2"
	+ "foo bar#snap 2" "foo bar@snap 2"
	R zfs hold zrepl_platformtest "${ROOTDS}/foo bar@snap 2"
	+ "foo bar@snap 3"
	+ "foo bar#snap 3" "foo bar@snap 3"
	R zfs hold zrepl_platformtest "${ROOTDS}/foo bar@snap 3"
	R zfs hold zrepl_platformtest_second_hold "${ROOTDS}/foo bar@snap 3"
	+ "foo bar@snap 4"
	+ "foo bar#snap 4" "foo bar@snap 4"


	+ "foo bar/child datset"
	+ "foo bar/child datset@snap 1"
	+ "foo bar/child datset#snap 1" "foo bar/child datset@snap 1"
	+ "foo bar/child datset@snap 2"
	+ "foo bar/child datset#snap 2" "foo bar/child datset@snap 2"
	R zfs hold zrepl_platformtest "${ROOTDS}/foo bar/child datset@snap 2"
	+ "foo bar/child datset@snap 3"
	+ "foo bar/child datset#snap 3" "foo bar/child datset@snap 3"
	R zfs hold zrepl_platformtest "${ROOTDS}/foo bar/child datset@snap 3"
	R zfs hold zrepl_platformtest_second_hold "${ROOTDS}/foo bar/child datset@snap 3"
	+ "foo bar/child datset@snap 4"
	+ "foo bar/child datset#snap 4" "foo bar/child datset@snap 4"
	`)

	fs := fmt.Sprintf("%s/foo bar", t.RootDataset)

	vs, err := zfs.ZFSListFilesystemVersions(t, mustDatasetPath(fs), zfs.ListFilesystemVersionsOptions{})
	require.NoError(t, err)

	type expectation struct {
		relName  string
		userrefs zfs.OptionUint64
	}

	expect := []expectation{
		{"#snap 1", zfs.OptionUint64{Valid: false}},
		{"#snap 2", zfs.OptionUint64{Valid: false}},
		{"#snap 3", zfs.OptionUint64{Valid: false}},
		{"#snap 4", zfs.OptionUint64{Valid: false}},
		{"@snap 1", zfs.OptionUint64{Value: 0, Valid: true}},
		{"@snap 2", zfs.OptionUint64{Value: 1, Valid: true}},
		{"@snap 3", zfs.OptionUint64{Value: 2, Valid: true}},
		{"@snap 4", zfs.OptionUint64{Value: 0, Valid: true}},
	}

	sort.Slice(vs, func(i, j int) bool {
		return strings.Compare(vs[i].RelName(), vs[j].RelName()) < 0
	})

	var expectRelNames []string
	for _, e := range expect {
		expectRelNames = append(expectRelNames, e.relName)
	}

	require.Equal(t, expectRelNames, versionRelnamesSorted(vs))

	for i, e := range expect {
		require.Equal(t, e.relName, vs[i].RelName())
		require.Equal(t, e.userrefs, vs[i].UserRefs)
	}

}
