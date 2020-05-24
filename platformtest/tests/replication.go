package tests

import (
	"context"
	"fmt"
	"io"
	"path"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/require"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/replication/logic"
	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/replication/report"
	"github.com/zrepl/zrepl/util/limitio"
	"github.com/zrepl/zrepl/zfs"
)

// mimics the replication invocations of an active-side job
// for a single sender-receiver filesystem pair
//
// each invocation of method Do results in the construction
// of a new sender and receiver instance and one blocking invocation
// of the replication engine without encryption
type replicationInvocation struct {
	sjid, rjid      endpoint.JobID
	sfs             string
	rfsRoot         string
	interceptSender func(e *endpoint.Sender) logic.Sender
}

func (i replicationInvocation) Do(ctx *platformtest.Context) *report.Report {

	if i.interceptSender == nil {
		i.interceptSender = func(e *endpoint.Sender) logic.Sender { return e }
	}

	sfilter := filters.NewDatasetMapFilter(1, true)
	err := sfilter.Add(i.sfs, "ok")
	require.NoError(ctx, err)
	sender := i.interceptSender(endpoint.NewSender(endpoint.SenderConfig{
		FSF:     sfilter.AsFilter(),
		Encrypt: &zfs.NilBool{B: false},
		JobID:   i.sjid,
	}))
	receiver := endpoint.NewReceiver(endpoint.ReceiverConfig{
		JobID:                      i.rjid,
		AppendClientIdentity:       false,
		RootWithoutClientComponent: mustDatasetPath(i.rfsRoot),
		UpdateLastReceivedHold:     true,
	})
	plannerPolicy := logic.PlannerPolicy{
		EncryptedSend: logic.TriFromBool(false),
	}

	report, wait := replication.Do(
		ctx,
		logic.NewPlanner(nil, nil, sender, receiver, plannerPolicy),
	)
	wait(true)
	return report()
}

func (i replicationInvocation) ReceiveSideFilesystem() string {
	return path.Join(i.rfsRoot, i.sfs)
}

func ReplicationIncrementalIsPossibleIfCommonSnapshotIsDestroyed(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "sender"
		+  "sender@1"
		+  "receiver"
		R  zfs create -p "${ROOTDS}/receiver/${ROOTDS}"
	`)

	sjid := endpoint.MustMakeJobID("sender-job")
	rjid := endpoint.MustMakeJobID("receiver-job")

	sfs := ctx.RootDataset + "/sender"
	rfsRoot := ctx.RootDataset + "/receiver"
	snap1 := fsversion(ctx, sfs, "@1")

	rep := replicationInvocation{
		sjid:    sjid,
		rjid:    rjid,
		sfs:     sfs,
		rfsRoot: rfsRoot,
	}
	rfs := rep.ReceiveSideFilesystem()

	// first replication
	report := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(report))

	// assert @1 exists on receiver
	_ = fsversion(ctx, rfs, "@1")

	// cut off the common base between sender and receiver
	// (replication engine guarantees resumability through bookmarks)
	err := zfs.ZFSDestroy(ctx, snap1.FullPath(sfs))
	require.NoError(ctx, err)

	// assert that the replication cursor has been created
	snap1CursorName, err := endpoint.ReplicationCursorBookmarkName(sfs, snap1.Guid, sjid)
	require.NoError(ctx, err)
	snap1CursorInfo, err := zfs.ZFSGetFilesystemVersion(ctx, sfs+"#"+snap1CursorName)
	require.NoError(ctx, err)
	require.True(ctx, snap1CursorInfo.IsBookmark())

	// second replication of a new snapshot, should use the cursor
	mustSnapshot(ctx, sfs+"@2")
	report = rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(report))
	_ = fsversion(ctx, rfs, "@2")

}

type PartialSender struct {
	*endpoint.Sender
	failAfterByteCount int64
}

var _ logic.Sender = (*PartialSender)(nil)

func (s *PartialSender) Send(ctx context.Context, r *pdu.SendReq) (r1 *pdu.SendRes, r2 io.ReadCloser, r3 error) {
	r1, r2, r3 = s.Sender.Send(ctx, r)
	r2 = limitio.ReadCloser(r2, s.failAfterByteCount)
	return r1, r2, r3
}

func ReplicationIsResumableFullSend(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "sender"
		+  "receiver"
		R  zfs create -p "${ROOTDS}/receiver/${ROOTDS}"
	`)

	sjid := endpoint.MustMakeJobID("sender-job")
	rjid := endpoint.MustMakeJobID("receiver-job")

	sfs := ctx.RootDataset + "/sender"
	rfsRoot := ctx.RootDataset + "/receiver"

	sfsmp, err := zfs.ZFSGetMountpoint(ctx, sfs)
	require.NoError(ctx, err)
	require.True(ctx, sfsmp.Mounted)

	writeDummyData(path.Join(sfsmp.Mountpoint, "dummy.data"), 1<<22)
	mustSnapshot(ctx, sfs+"@1")
	snap1 := fsversion(ctx, sfs, "@1")

	rep := replicationInvocation{
		sjid:    sjid,
		rjid:    rjid,
		sfs:     sfs,
		rfsRoot: rfsRoot,
		interceptSender: func(e *endpoint.Sender) logic.Sender {
			return &PartialSender{Sender: e, failAfterByteCount: 1 << 20}
		},
	}
	rfs := rep.ReceiveSideFilesystem()

	for i := 2; i < 10; i++ {
		report := rep.Do(ctx)
		ctx.Logf("\n%s", pretty.Sprint(report))

		// always attempt to destroy the incremental source
		err := zfs.ZFSDestroy(ctx, snap1.FullPath(sfs))
		if i < 4 {
			// we configured the PartialSender to fail after 1<<20 bytes
			// and we wrote dummy data 1<<22 bytes, thus at least
			// for the first 4 times this should not be possible
			// due to step holds
			require.Error(ctx, err)
			require.Contains(ctx, err.Error(), "dataset is busy")
		}

		// and create some additional snapshots that could
		// confuse a naive implementation that doesn't take into
		// account resume state when planning replication
		if i == 2 || i == 3 {
			// no significant size to avoid making this test run longer than necessary
			mustSnapshot(ctx, fmt.Sprintf("%s@%d", sfs, i))
		}

		require.Len(ctx, report.Attempts, 1)
		require.Nil(ctx, report.Attempts[0].PlanError)
		require.Len(ctx, report.Attempts[0].Filesystems, 1)
		if len(report.Attempts[0].Filesystems[0].Steps) == 0 {
			break
		}
	}

	// make sure all the filesystem versions we created
	// were replicated by the replication loop
	_ = fsversion(ctx, rfs, "@1")
	_ = fsversion(ctx, rfs, "@2")
	_ = fsversion(ctx, rfs, "@3")

}
