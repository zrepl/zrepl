package tests

import (
	"fmt"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/internal/endpoint"
	"github.com/zrepl/zrepl/internal/platformtest"
	"github.com/zrepl/zrepl/internal/replication/logic/pdu"
	"github.com/zrepl/zrepl/internal/util/bandwidthlimit"
	"github.com/zrepl/zrepl/internal/util/nodefault"
	"github.com/zrepl/zrepl/internal/zfs"
)

func DestroySnapshotsRejectsSelectors(ctx *platformtest.Context) {
	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+ "fs"
		+ "fs@s1"
		+ "fs@s2"
		+ "fs@s3"
	`)
	fs := fmt.Sprintf("%s/fs", ctx.RootDataset)
	sender := endpoint.NewSender(endpoint.SenderConfig{
		JobID:          endpoint.MustMakeJobID("destroy-validation-sender"),
		FSF:            zfs.NoFilter(),
		Encrypt:        &nodefault.Bool{B: false},
		BandwidthLimit: bandwidthlimit.NoLimitConfig(),
	})
	receiver := endpoint.NewReceiver(endpoint.ReceiverConfig{
		JobID:                      endpoint.MustMakeJobID("destroy-validation-receiver"),
		RootWithoutClientComponent: mustDatasetPath(ctx.RootDataset),
		BandwidthLimit:             bandwidthlimit.NoLimitConfig(),
		PlaceholderEncryption:      endpoint.PlaceholderCreationEncryptionPropertyUnspecified,
	})
	s1 := fsversion(ctx, fs, "@s1")
	for _, tc := range []struct {
		endpoint   pdu.ReplicationServer
		filesystem string
	}{
		{sender, fs},
		{receiver, "fs"},
	} {
		for _, name := range []string{"%", "s1%s3", "s1,s3"} {
			_, err := tc.endpoint.DestroySnapshots(ctx, &pdu.DestroySnapshotsReq{
				Filesystem: tc.filesystem,
				Snapshots: []*pdu.FilesystemVersion{pdu.FilesystemVersionFromZFS(&s1), {
					Type: pdu.FilesystemVersion_Snapshot,
					Name: name,
				}},
			})
			require.Error(ctx, err, "snapshot selectors must be rejected before destroying anything")
			for _, relname := range []string{"@s1", "@s2", "@s3"} {
				fsversion(ctx, fs, relname)
			}
		}
	}
	s3 := fsversion(ctx, fs, "@s3")
	res, err := receiver.DestroySnapshots(ctx, &pdu.DestroySnapshotsReq{
		Filesystem: "fs",
		Snapshots:  []*pdu.FilesystemVersion{pdu.FilesystemVersionFromZFS(&s1), pdu.FilesystemVersionFromZFS(&s3)},
	})
	require.NoError(ctx, err)
	require.Len(ctx, res.Results, 2)
	for _, result := range res.Results {
		require.Empty(ctx, result.Error)
	}
	versions, err := zfs.ZFSListFilesystemVersions(ctx, mustDatasetPath(fs), zfs.ListFilesystemVersionsOptions{Types: zfs.Snapshots})
	require.NoError(ctx, err)
	require.Len(ctx, versions, 1)
	require.Equal(ctx, "s2", versions[0].Name)
}
