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
	sjid, rjid                  endpoint.JobID
	sfs                         string
	rfsRoot                     string
	interceptSender             func(e *endpoint.Sender) logic.Sender
	disableIncrementalStepHolds bool
}

func (i replicationInvocation) Do(ctx *platformtest.Context) *report.Report {

	if i.interceptSender == nil {
		i.interceptSender = func(e *endpoint.Sender) logic.Sender { return e }
	}

	sfilter := filters.NewDatasetMapFilter(1, true)
	err := sfilter.Add(i.sfs, "ok")
	require.NoError(ctx, err)
	sender := i.interceptSender(endpoint.NewSender(endpoint.SenderConfig{
		FSF:                         sfilter.AsFilter(),
		Encrypt:                     &zfs.NilBool{B: false},
		DisableIncrementalStepHolds: i.disableIncrementalStepHolds,
		JobID:                       i.sjid,
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
		sjid:                        sjid,
		rjid:                        rjid,
		sfs:                         sfs,
		rfsRoot:                     rfsRoot,
		disableIncrementalStepHolds: false,
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

func ReplicationIncrementalCleansUpStaleAbstractionsWithCacheOnSecondReplication(ctx *platformtest.Context) {
	implReplicationIncrementalCleansUpStaleAbstractions(ctx, true)
}

func ReplicationIncrementalCleansUpStaleAbstractionsWithoutCacheOnSecondReplication(ctx *platformtest.Context) {
	implReplicationIncrementalCleansUpStaleAbstractions(ctx, false)
}

func implReplicationIncrementalCleansUpStaleAbstractions(ctx *platformtest.Context, invalidateCacheBeforeSecondReplication bool) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "sender"
		+  "sender@1"
		+  "sender@2"
		+  "sender#2" "sender@2"
		+  "sender@3"
		+  "receiver"
		R  zfs create -p "${ROOTDS}/receiver/${ROOTDS}"
	`)

	sjid := endpoint.MustMakeJobID("sender-job")
	ojid := endpoint.MustMakeJobID("other-job")
	rjid := endpoint.MustMakeJobID("receiver-job")

	sfs := ctx.RootDataset + "/sender"
	rfsRoot := ctx.RootDataset + "/receiver"

	rep := replicationInvocation{
		sjid:                        sjid,
		rjid:                        rjid,
		sfs:                         sfs,
		rfsRoot:                     rfsRoot,
		disableIncrementalStepHolds: false,
	}
	rfs := rep.ReceiveSideFilesystem()

	// first replication
	report := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(report))

	// assert most recent send-side version @3 exists on receiver (=replication succeeded)
	rSnap3 := fsversion(ctx, rfs, "@3")
	// assert the source-side versions not managed by zrepl still exist
	snap1 := fsversion(ctx, sfs, "@1")
	snap2 := fsversion(ctx, sfs, "@2")
	_ = fsversion(ctx, sfs, "#2") // non-replicationc-cursor bookmarks should not be affected
	snap3 := fsversion(ctx, sfs, "@3")
	// assert a replication cursor is in place
	snap3CursorName, err := endpoint.ReplicationCursorBookmarkName(sfs, snap3.Guid, sjid)
	require.NoError(ctx, err)
	_ = fsversion(ctx, sfs, "#"+snap3CursorName)
	// assert a last-received hold is in place
	expectRjidHoldTag, err := endpoint.LastReceivedHoldTag(rjid)
	require.NoError(ctx, err)
	holds, err := zfs.ZFSHolds(ctx, rfs, rSnap3.Name)
	require.NoError(ctx, err)
	require.Contains(ctx, holds, expectRjidHoldTag)

	// create artifical stale replication cursors
	createArtificalStaleAbstractions := func(jobId endpoint.JobID) []endpoint.Abstraction {
		snap2Cursor, err := endpoint.CreateReplicationCursor(ctx, sfs, snap2, jobId) // no shadow
		require.NoError(ctx, err)
		// create artifical stale step holds jobId
		snap1Hold, err := endpoint.HoldStep(ctx, sfs, snap1, jobId) // no shadow
		require.NoError(ctx, err)
		snap2Hold, err := endpoint.HoldStep(ctx, sfs, snap2, jobId) // no shadow
		require.NoError(ctx, err)
		return []endpoint.Abstraction{snap2Cursor, snap1Hold, snap2Hold}
	}
	createArtificalStaleAbstractions(sjid)
	ojidSendAbstractions := createArtificalStaleAbstractions(ojid)

	snap3ojidLastReceivedHold, err := endpoint.CreateLastReceivedHold(ctx, rfs, fsversion(ctx, rfs, "@3"), ojid)
	require.NoError(ctx, err)
	require.True(ctx, zfs.FilesystemVersionEqualIdentity(fsversion(ctx, rfs, "@3"), snap3ojidLastReceivedHold.GetFilesystemVersion()))

	// take another 2 snapshots
	mustSnapshot(ctx, sfs+"@4")
	mustSnapshot(ctx, sfs+"@5")
	snap5 := fsversion(ctx, sfs, "@5")

	if invalidateCacheBeforeSecondReplication {
		endpoint.SendAbstractionsCacheInvalidate(sfs)
	}

	// do another replication
	// - ojid's abstractions should not be affected on either side
	// - stale abstractions of sjid and rjid should be cleaned up
	// - 1 replication cursors and 1 last-received hold should be present

	checkOjidAbstractionsExist := func() {
		var expectedOjidAbstractions []endpoint.Abstraction
		expectedOjidAbstractions = append(expectedOjidAbstractions, ojidSendAbstractions...)
		expectedOjidAbstractions = append(expectedOjidAbstractions, snap3ojidLastReceivedHold)

		sfsAndRfsFilter := filters.NewDatasetMapFilter(2, true)
		require.NoError(ctx, sfsAndRfsFilter.Add(sfs, "ok"))
		require.NoError(ctx, sfsAndRfsFilter.Add(rfs, "ok"))
		rAbs, rAbsErrs, err := endpoint.ListAbstractions(ctx, endpoint.ListZFSHoldsAndBookmarksQuery{
			FS:          endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter{Filter: sfsAndRfsFilter},
			JobID:       &ojid,
			What:        endpoint.AbstractionTypesAll,
			Concurrency: 1,
		})
		require.NoError(ctx, err)
		require.Len(ctx, rAbsErrs, 0)
		ctx.Logf("rAbs=%s", rAbs)
		ctx.Logf("expectedOjidAbstractions=%s", expectedOjidAbstractions)
		require.Equal(ctx, len(expectedOjidAbstractions), len(rAbs))
		for _, ea := range expectedOjidAbstractions {
			ctx.Logf("looking for %s %#v", ea, ea.GetFilesystemVersion())
			found := false
			for _, a := range rAbs {
				eq := endpoint.AbstractionEquals(ea, a)
				ctx.Logf("comp=%v for %s %#v", eq, a, a.GetFilesystemVersion())
				found = found || eq
			}
			require.True(ctx, found, "%s", ea)
		}
	}
	checkOjidAbstractionsExist()

	report = rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(report))

	checkOjidAbstractionsExist()

	_ = fsversion(ctx, sfs, "@1")
	_ = fsversion(ctx, sfs, "@2")
	_ = fsversion(ctx, sfs, "#2")
	_ = fsversion(ctx, sfs, "@3")
	_ = fsversion(ctx, sfs, "@4")
	_ = fsversion(ctx, sfs, "@5")

	_ = fsversion(ctx, rfs, "@3")
	_ = fsversion(ctx, rfs, "@4")
	_ = fsversion(ctx, rfs, "@5")

	// check bookmark situation
	{
		sBms, err := zfs.ZFSListFilesystemVersions(ctx, mustDatasetPath(sfs), zfs.ListFilesystemVersionsOptions{
			Types: zfs.Bookmarks,
		})
		ctx.Logf("sbms=%s", sBms)
		require.NoError(ctx, err)

		snap5SjidCursorName, err := endpoint.ReplicationCursorBookmarkName(sfs, snap5.Guid, sjid)
		require.NoError(ctx, err)
		snap2SjidCursorName, err := endpoint.ReplicationCursorBookmarkName(sfs, snap2.Guid, sjid)
		require.NoError(ctx, err)
		snap2OjidCursorName, err := endpoint.ReplicationCursorBookmarkName(sfs, snap2.Guid, ojid)
		require.NoError(ctx, err)
		var bmNames []string
		for _, bm := range sBms {
			bmNames = append(bmNames, bm.Name)
		}

		if invalidateCacheBeforeSecondReplication {
			require.Len(ctx, sBms, 3)
			require.Contains(ctx, bmNames, snap5SjidCursorName)
			require.Contains(ctx, bmNames, snap2OjidCursorName)
			require.Contains(ctx, bmNames, "2")
		} else {
			require.Len(ctx, sBms, 4)
			require.Contains(ctx, bmNames, snap5SjidCursorName)
			require.Contains(ctx, bmNames, snap2SjidCursorName)
			require.Contains(ctx, bmNames, snap2OjidCursorName)
			require.Contains(ctx, bmNames, "2")
		}
	}

	// check last-received hold moved
	{
		rAbs, rAbsErrs, err := endpoint.ListAbstractions(ctx, endpoint.ListZFSHoldsAndBookmarksQuery{
			FS:          endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter{FS: &rfs},
			JobID:       &rjid,
			What:        endpoint.AbstractionTypesAll,
			Concurrency: 1,
		})
		require.NoError(ctx, err)
		require.Len(ctx, rAbsErrs, 0)
		require.Len(ctx, rAbs, 1)
		require.Equal(ctx, rAbs[0].GetType(), endpoint.AbstractionLastReceivedHold)
		require.Equal(ctx, *rAbs[0].GetJobID(), rjid)
		require.Equal(ctx, rAbs[0].GetFilesystemVersion().GetGuid(), snap5.GetGuid())
	}

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

func ReplicationIsResumableFullSend__DisableIncrementalStepHolds_False(ctx *platformtest.Context) {
	implReplicationIsResumableFullSend(ctx, false)
}

func ReplicationIsResumableFullSend__DisableIncrementalStepHolds_True(ctx *platformtest.Context) {
	implReplicationIsResumableFullSend(ctx, true)
}

func implReplicationIsResumableFullSend(ctx *platformtest.Context, disableIncrementalStepHolds bool) {

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
		disableIncrementalStepHolds: disableIncrementalStepHolds,
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
