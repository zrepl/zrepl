package endpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/internal/daemon/logging/trace"
	"github.com/zrepl/zrepl/internal/replication/logic/pdu"
	"github.com/zrepl/zrepl/internal/zfs"
)

func TestDestroySnapshotsRejectsInvalidVersions(t *testing.T) {
	ctx, done := trace.WithTask(context.Background(), t.Name())
	defer done()
	dir := t.TempDir()
	binary := filepath.Join(dir, "zfs")
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\n: > \"$ZREPL_TEST_DESTROY_LOG\"\nexit 1\n"), 0700))
	previousBinary := zfs.ZFS_BINARY
	zfs.ZFS_BINARY = binary
	t.Cleanup(func() { zfs.ZFS_BINARY = previousBinary })
	fs, err := zfs.NewDatasetPath("pool/fs")
	require.NoError(t, err)
	snapshot := func(name string) *pdu.FilesystemVersion {
		return &pdu.FilesystemVersion{Type: pdu.FilesystemVersion_Snapshot, Name: name}
	}
	for _, tc := range []struct {
		name    string
		version *pdu.FilesystemVersion
	}{
		{"all", snapshot("%")},
		{"range", snapshot("s1%s2")},
		{"list", snapshot("s1,s2")},
		{"empty", snapshot("")},
		{"snapshot delimiter", snapshot("s1@s2")},
		{"filesystem delimiter", snapshot("fs/s1")},
		{"bookmark delimiter", snapshot("s1#s2")},
		{"nil", nil},
		{"bookmark", &pdu.FilesystemVersion{Type: pdu.FilesystemVersion_Bookmark, Name: "s1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "zfs-invoked")
			t.Setenv("ZREPL_TEST_DESTROY_LOG", log)
			var res *pdu.DestroySnapshotsRes
			var err error
			require.NotPanics(t, func() {
				res, err = doDestroySnapshots(ctx, fs, []*pdu.FilesystemVersion{snapshot("valid"), tc.version})
			})
			require.Error(t, err)
			require.Nil(t, res)
			require.NoFileExists(t, log, "the entire request must be validated before invoking ZFS")
		})
	}
}
