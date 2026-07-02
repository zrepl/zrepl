package tests

import (
	"fmt"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/internal/endpoint"
	"github.com/zrepl/zrepl/internal/platformtest"
	"github.com/zrepl/zrepl/internal/pruning"
	"github.com/zrepl/zrepl/internal/replication/logic/pdu"
	"github.com/zrepl/zrepl/internal/util/bandwidthlimit"
	"github.com/zrepl/zrepl/internal/zfs"
)

func SinkLocalPruningFloorProtectsSnapshots(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "sinkroot"
		+  "sinkroot/fs"
		+  "sinkroot/fs@zrepl_1"
		+  "sinkroot/fs@zrepl_2"
		+  "sinkroot/fs@manual_1"
	`)

	fs := fmt.Sprintf("%s/sinkroot/fs", ctx.RootDataset)

	keepZrepl, err := pruning.NewKeepRegex("^zrepl_.*", false)
	require.NoError(ctx, err)

	receiver := endpoint.NewReceiver(endpoint.ReceiverConfig{
		JobID:                      endpoint.MustMakeJobID("sink-floor-test"),
		AppendClientIdentity:       false,
		RootWithoutClientComponent: mustDatasetPath(fmt.Sprintf("%s/sinkroot", ctx.RootDataset)),
		BandwidthLimit:             bandwidthlimit.NoLimitConfig(),
		PlaceholderEncryption:      endpoint.PlaceholderCreationEncryptionPropertyUnspecified,
		Keep:                       []pruning.KeepRule{keepZrepl},
	})

	mustFsv := func(relname string) *pdu.FilesystemVersion {
		v := fsversion(ctx, fs, relname)
		return pdu.FilesystemVersionFromZFS(&v)
	}
	protectedSnap := mustFsv("@zrepl_1")
	unprotectedSnap := mustFsv("@manual_1")
	staleSnap := &pdu.FilesystemVersion{
		Type: pdu.FilesystemVersion_Snapshot,
		Name: "zrepl_gone",
	}

	res, err := receiver.DestroySnapshots(ctx, &pdu.DestroySnapshotsReq{
		Filesystem: "fs",
		Snapshots:  []*pdu.FilesystemVersion{protectedSnap, unprotectedSnap, staleSnap},
	})
	require.NoError(ctx, err)

	// the caller (active-side pruner) requires one result per requested snapshot
	require.Len(ctx, res.Results, 3)
	resultsByName := make(map[string]*pdu.DestroySnapshotRes, len(res.Results))
	for _, r := range res.Results {
		resultsByName[r.Snapshot.Name] = r
	}

	// protected snapshot: reported as success, but must still exist
	require.Contains(ctx, resultsByName, "zrepl_1")
	require.Empty(ctx, resultsByName["zrepl_1"].Error)

	// unprotected snapshot: actually destroyed
	require.Contains(ctx, resultsByName, "manual_1")
	require.Empty(ctx, resultsByName["manual_1"].Error)

	// nonexistent snapshot: passed through to the batch destroy
	require.Contains(ctx, resultsByName, "zrepl_gone")

	versions, err := zfs.ZFSListFilesystemVersions(ctx, mustDatasetPath(fs), zfs.ListFilesystemVersionsOptions{
		Types: zfs.Snapshots,
	})
	require.NoError(ctx, err)
	remaining := make(map[string]bool, len(versions))
	for _, v := range versions {
		remaining[v.Name] = true
	}
	require.True(ctx, remaining["zrepl_1"], "snapshot protected by local floor policy must survive")
	require.True(ctx, remaining["zrepl_2"], "snapshot not requested for destruction must survive")
	require.False(ctx, remaining["manual_1"], "snapshot not protected by local floor policy must be destroyed")

	// a nonexistent snapshot must yield the usual zfs error, proving it was
	// passed through to the destroy and not misclassified as protected
	res, err = receiver.DestroySnapshots(ctx, &pdu.DestroySnapshotsReq{
		Filesystem: "fs",
		Snapshots:  []*pdu.FilesystemVersion{staleSnap},
	})
	require.NoError(ctx, err)
	require.Len(ctx, res.Results, 1)
	require.NotEmpty(ctx, res.Results[0].Error)
}
