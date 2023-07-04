package tests

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/replication/driver"
	"github.com/zrepl/zrepl/replication/logic"
	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/replication/report"
	"github.com/zrepl/zrepl/util/bandwidthlimit"
	"github.com/zrepl/zrepl/util/limitio"
	"github.com/zrepl/zrepl/util/nodefault"
	"github.com/zrepl/zrepl/zfs"
	zfsprop "github.com/zrepl/zrepl/zfs/property"
)

// mimics the replication invocations of an active-side job
// for a single sender-receiver filesystem pair
//
// each invocation of method Do results in the construction
// of a new sender and receiver instance and one blocking invocation
// of the replication engine without encryption
type replicationInvocation struct {
	sjid, rjid             endpoint.JobID
	sfs                    string
	sfilter                *filters.DatasetMapFilter
	rfsRoot                string
	interceptSender        func(e *endpoint.Sender) logic.Sender
	interceptReceiver      func(e *endpoint.Receiver) logic.Receiver
	guarantee              *pdu.ReplicationConfigProtection
	senderConfigHook       func(*endpoint.SenderConfig)
	receiverConfigHook     func(*endpoint.ReceiverConfig)
	plannerPolicyHook      func(*logic.PlannerPolicy)
	skipSendArgsValidation bool
}

func (i replicationInvocation) Do(ctx *platformtest.Context) *report.Report {

	if i.interceptSender == nil {
		i.interceptSender = func(e *endpoint.Sender) logic.Sender { return e }
	}
	if i.interceptReceiver == nil {
		i.interceptReceiver = func(e *endpoint.Receiver) logic.Receiver { return e }
	}

	if i.sfs != "" && i.sfilter != nil || i.sfs == "" && i.sfilter == nil {
		panic("either sfs or sfilter must be set")
	}
	if i.sfilter == nil {
		i.sfilter = filters.NewDatasetMapFilter(1, true)
		err := i.sfilter.Add(i.sfs, "ok")
		require.NoError(ctx, err)
	}

	senderConfig := endpoint.SenderConfig{
		FSF:            i.sfilter.AsFilter(),
		Encrypt:        &nodefault.Bool{B: false},
		JobID:          i.sjid,
		BandwidthLimit: bandwidthlimit.NoLimitConfig(),
	}
	if i.senderConfigHook != nil {
		i.senderConfigHook(&senderConfig)
	}

	receiverConfig := endpoint.ReceiverConfig{
		JobID:                      i.rjid,
		AppendClientIdentity:       false,
		RootWithoutClientComponent: mustDatasetPath(i.rfsRoot),
		BandwidthLimit:             bandwidthlimit.NoLimitConfig(),
		PlaceholderEncryption:      endpoint.PlaceholderCreationEncryptionPropertyUnspecified,
	}
	if i.receiverConfigHook != nil {
		i.receiverConfigHook(&receiverConfig)
	}

	require.Equal(ctx, senderConfig.JobID, i.sjid)
	require.Equal(ctx, receiverConfig.JobID, i.rjid)

	sender := i.interceptSender(endpoint.NewSender(senderConfig))
	receiver := i.interceptReceiver(endpoint.NewReceiver(receiverConfig))
	plannerPolicy := logic.PlannerPolicy{
		ReplicationConfig: &pdu.ReplicationConfig{
			Protection: i.guarantee,
		},
		ConflictResolution: &logic.ConflictResolution{
			InitialReplication: logic.InitialReplicationAutoResolutionMostRecent,
		},
		SizeEstimationConcurrency: 1,
	}
	if i.plannerPolicyHook != nil {
		i.plannerPolicyHook(&plannerPolicy)
	}

	var doCtx context.Context = ctx
	if i.skipSendArgsValidation {
		doCtx = zfs.ZFSSendArgsSkipValidation(ctx)
	}

	report, wait := replication.Do(
		doCtx,
		driver.Config{
			MaxAttempts:              1,
			StepQueueConcurrency:     1,
			ReconnectHardFailTimeout: 1 * time.Second,
		},
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
		sjid:      sjid,
		rjid:      rjid,
		sfs:       sfs,
		rfsRoot:   rfsRoot,
		guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeResumability),
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
		sjid:      sjid,
		rjid:      rjid,
		sfs:       sfs,
		rfsRoot:   rfsRoot,
		guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeResumability),
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

	// create artifical stale replication cursors & step holds
	createArtificalStaleAbstractions := func(jobId endpoint.JobID) []endpoint.Abstraction {
		snap2Cursor, err := endpoint.CreateReplicationCursor(ctx, sfs, snap2, jobId) // no shadow
		require.NoError(ctx, err)
		// create artifical stale step holds jobId
		snap1Hold, err := endpoint.HoldStep(ctx, sfs, snap1, jobId) // no shadow
		require.NoError(ctx, err)
		snap2Hold, err := endpoint.HoldStep(ctx, sfs, snap2, jobId) // no shadow
		require.NoError(ctx, err)
		// create artificial tentative cursor
		snap3TentativeCursor, err := endpoint.CreateTentativeReplicationCursor(ctx, sfs, snap3, jobId)
		require.NoError(ctx, err)
		return []endpoint.Abstraction{snap2Cursor, snap1Hold, snap2Hold, snap3TentativeCursor}
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
		endpoint.AbstractionsCacheInvalidate(sfs)
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
		snap3SjidTentativeCursorName, err := endpoint.TentativeReplicationCursorBookmarkName(sfs, snap3.Guid, sjid)
		require.NoError(ctx, err)
		snap3OjidTentativeCursorName, err := endpoint.TentativeReplicationCursorBookmarkName(sfs, snap3.Guid, ojid)
		require.NoError(ctx, err)
		var bmNames []string
		for _, bm := range sBms {
			bmNames = append(bmNames, bm.Name)
		}

		if invalidateCacheBeforeSecondReplication {
			require.Len(ctx, sBms, 4)
			require.Contains(ctx, bmNames, snap5SjidCursorName)
			require.Contains(ctx, bmNames, snap2OjidCursorName)
			require.Contains(ctx, bmNames, snap3OjidTentativeCursorName)
			require.Contains(ctx, bmNames, "2")
		} else {
			require.Len(ctx, sBms, 6)
			ctx.Logf("%s", pretty.Sprint(sBms))
			require.Contains(ctx, bmNames, snap5SjidCursorName)
			require.Contains(ctx, bmNames, snap2SjidCursorName)
			require.Contains(ctx, bmNames, snap2OjidCursorName)
			require.Contains(ctx, bmNames, snap3SjidTentativeCursorName)
			require.Contains(ctx, bmNames, snap3OjidTentativeCursorName)
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

func ReplicationIncrementalHandlesFromVersionEqTentativeCursorCorrectly(ctx *platformtest.Context) {

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

	rep := replicationInvocation{
		sjid:    sjid,
		rjid:    rjid,
		sfs:     sfs,
		rfsRoot: rfsRoot,
		// It doesn't really matter what guarantee we use here, as the second replication will configure another.
		// But, in the real world, the only way for a stale tentative cursor to appear is if the guarantee is set to
		// incremental replication and we crash before converting the tentative cursor into a regular cursor.
		guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeIncrementalReplication),
	}

	// Do initial replication to set up the test.
	rep1 := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(rep1))
	sfsDs := mustDatasetPath(sfs)
	snap1_sender := mustGetFilesystemVersion(ctx, sfs+"@1")
	snap1_replicationCursor_name, err := endpoint.ReplicationCursorBookmarkName(sfs, snap1_sender.Guid, sjid)
	require.NoError(ctx, err)
	snap1_replicationCursor := mustGetFilesystemVersion(ctx, sfs+"#"+snap1_replicationCursor_name)

	// The second replication will be done with a guarantee kind that doesn't create tentative cursors by itself.
	// So, it would generally be right to clean up any tentative cursors on sfs since they're stale abstractions.
	// However, if the cursor is used as the `from` version in any send step, we must not destroy it, as that
	// would break incremental replication.
	// NB: we only need to test the first step as all subsequent steps will be snapshot->snapshot.
	rep.guarantee = pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeNothing)
	// create the artificial cursor
	snap1_tentativeCursor, err := endpoint.CreateTentativeReplicationCursor(ctx, sfs, snap1_sender, sjid)
	require.NoError(ctx, err)
	endpoint.AbstractionsCacheInvalidate(sfs)
	// remove other bookmarks of snap1, and snap1 itself, to force the replication planner to use the tentative cursor
	err = zfs.ZFSDestroyFilesystemVersion(ctx, sfsDs, &snap1_sender)
	require.NoError(ctx, err)
	err = zfs.ZFSDestroyFilesystemVersion(ctx, sfsDs, &snap1_replicationCursor)
	require.NoError(ctx, err)
	versions, err := zfs.ZFSListFilesystemVersions(ctx, sfsDs, zfs.ListFilesystemVersionsOptions{})
	require.NoError(ctx, err)
	require.Len(ctx, versions, 1)
	require.Equal(ctx, versions[0].Guid, snap1_tentativeCursor.GetFilesystemVersion().Guid)
	// create another snapshot so that replication does one incremental step `tentative_cursor` -> `@2`
	mustSnapshot(ctx, sfs+"@2")
	mustGetFilesystemVersion(ctx, sfs+"@2")
	// do the replication
	rep2 := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(rep2))

	// Ensure that the tentative cursor was used.
	require.Len(ctx, rep2.Attempts, 1)
	require.Equal(ctx, rep2.Attempts[0].State, report.AttemptDone)
	require.Len(ctx, rep2.Attempts[0].Filesystems, 1)
	require.Nil(ctx, rep2.Attempts[0].Filesystems[0].Error())
	require.Len(ctx, rep2.Attempts[0].Filesystems[0].Steps, 1)
	require.EqualValues(ctx, rep2.Attempts[0].Filesystems[0].CurrentStep, 1)
	require.Len(ctx, rep2.Attempts[0].Filesystems[0].Steps, 1)
	require.Equal(ctx, rep2.Attempts[0].Filesystems[0].Steps[0].Info.From, snap1_tentativeCursor.GetFilesystemVersion().RelName())

	// Ensure that the tentative cursor was destroyed as part of SendPost.
	_, err = zfs.ZFSGetFilesystemVersion(ctx, snap1_replicationCursor.FullPath(sfs))
	_, ok := err.(*zfs.DatasetDoesNotExist)
	require.True(ctx, ok)
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

func ReplicationIsResumableFullSend__both_GuaranteeResumability(ctx *platformtest.Context) {

	setup := replicationIsResumableFullSendSetup{
		protection: &pdu.ReplicationConfigProtection{
			Initial:     pdu.ReplicationGuaranteeKind_GuaranteeResumability,
			Incremental: pdu.ReplicationGuaranteeKind_GuaranteeResumability,
		},
		expectDatasetIsBusyErrorWhenDestroySnapshotWhilePartiallyReplicated: true,
		expectAllThreeSnapshotsToThreeBePresentAfterLoop:                    true,
		expectNoSnapshotsOnReceiverAfterLoop:                                false,
	}

	implReplicationIsResumableFullSend(ctx, setup)
}

func ReplicationIsResumableFullSend__initial_GuaranteeResumability_incremental_GuaranteeIncrementalReplication(ctx *platformtest.Context) {

	setup := replicationIsResumableFullSendSetup{
		protection: &pdu.ReplicationConfigProtection{
			Initial:     pdu.ReplicationGuaranteeKind_GuaranteeResumability,
			Incremental: pdu.ReplicationGuaranteeKind_GuaranteeIncrementalReplication,
		},
		expectDatasetIsBusyErrorWhenDestroySnapshotWhilePartiallyReplicated: true,
		expectAllThreeSnapshotsToThreeBePresentAfterLoop:                    true,
		expectNoSnapshotsOnReceiverAfterLoop:                                false,
	}

	implReplicationIsResumableFullSend(ctx, setup)
}

func ReplicationIsResumableFullSend__initial_GuaranteeIncrementalReplication_incremental_GuaranteeIncrementalReplication(ctx *platformtest.Context) {

	setup := replicationIsResumableFullSendSetup{
		protection: &pdu.ReplicationConfigProtection{
			Initial:     pdu.ReplicationGuaranteeKind_GuaranteeIncrementalReplication,
			Incremental: pdu.ReplicationGuaranteeKind_GuaranteeIncrementalReplication,
		},
		expectDatasetIsBusyErrorWhenDestroySnapshotWhilePartiallyReplicated: false,
		expectAllThreeSnapshotsToThreeBePresentAfterLoop:                    false,
		expectNoSnapshotsOnReceiverAfterLoop:                                true,
	}

	implReplicationIsResumableFullSend(ctx, setup)
}

type replicationIsResumableFullSendSetup struct {
	protection                                                          *pdu.ReplicationConfigProtection
	expectDatasetIsBusyErrorWhenDestroySnapshotWhilePartiallyReplicated bool
	expectAllThreeSnapshotsToThreeBePresentAfterLoop                    bool
	expectNoSnapshotsOnReceiverAfterLoop                                bool
}

func implReplicationIsResumableFullSend(ctx *platformtest.Context, setup replicationIsResumableFullSendSetup) {

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
		guarantee: setup.protection,
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
			if setup.expectDatasetIsBusyErrorWhenDestroySnapshotWhilePartiallyReplicated {
				ctx.Logf("i=%v", i)
				require.Error(ctx, err)
				require.Contains(ctx, err.Error(), "dataset is busy")
			}
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

	if setup.expectAllThreeSnapshotsToThreeBePresentAfterLoop {
		// make sure all the filesystem versions we created
		// were replicated by the replication loop
		_ = fsversion(ctx, rfs, "@1")
		_ = fsversion(ctx, rfs, "@2")
		_ = fsversion(ctx, rfs, "@3")
	}

	if setup.expectNoSnapshotsOnReceiverAfterLoop {
		versions, err := zfs.ZFSListFilesystemVersions(ctx, mustDatasetPath(rfs), zfs.ListFilesystemVersionsOptions{})
		require.NoError(ctx, err)
		require.Empty(ctx, versions)
	}

}

func ReplicationIncrementalDestroysStepHoldsIffIncrementalStepHoldsAreDisabledButStepHoldsExist(ctx *platformtest.Context) {

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

	// fully replicate snapshots @1
	{
		mustSnapshot(ctx, sfs+"@1")
		rep := replicationInvocation{
			sjid:      sjid,
			rjid:      rjid,
			sfs:       sfs,
			rfsRoot:   rfsRoot,
			guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeResumability),
		}
		rfs := rep.ReceiveSideFilesystem()
		report := rep.Do(ctx)
		ctx.Logf("\n%s", pretty.Sprint(report))
		// assert this worked (not the main subject of the test)
		_ = fsversion(ctx, rfs, "@1")
	}

	// create a large snapshot @2
	{
		sfsmp, err := zfs.ZFSGetMountpoint(ctx, sfs)
		require.NoError(ctx, err)
		require.True(ctx, sfsmp.Mounted)
		writeDummyData(path.Join(sfsmp.Mountpoint, "dummy.data"), 1<<22)
		mustSnapshot(ctx, sfs+"@2")
	}
	snap2sfs := fsversion(ctx, sfs, "@2")

	// partially replicate snapshots @2 with step holds enabled
	// to effect a step-holds situation
	{
		rep := replicationInvocation{
			sjid:      sjid,
			rjid:      rjid,
			sfs:       sfs,
			rfsRoot:   rfsRoot,
			guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeResumability), // !
			interceptSender: func(e *endpoint.Sender) logic.Sender {
				return &PartialSender{Sender: e, failAfterByteCount: 1 << 20}
			},
		}
		rfs := rep.ReceiveSideFilesystem()
		report := rep.Do(ctx)
		ctx.Logf("\n%s", pretty.Sprint(report))
		// assert this partial receive worked
		_, err := zfs.ZFSGetFilesystemVersion(ctx, rfs+"@2")
		ctx.Logf("%T %s", err, err)
		_, notFullyReceived := err.(*zfs.DatasetDoesNotExist)
		require.True(ctx, notFullyReceived)
		// assert step holds are in place
		abs, absErrs, err := endpoint.ListAbstractions(ctx, endpoint.ListZFSHoldsAndBookmarksQuery{
			FS: endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter{
				FS: &sfs,
			},
			Concurrency: 1,
			JobID:       &sjid,
			What:        endpoint.AbstractionTypeSet{endpoint.AbstractionStepHold: true},
		})
		require.NoError(ctx, err)
		require.Empty(ctx, absErrs)
		require.Len(ctx, abs, 2)
		sort.Slice(abs, func(i, j int) bool {
			return abs[i].GetCreateTXG() < abs[j].GetCreateTXG()
		})
		require.True(ctx, zfs.FilesystemVersionEqualIdentity(abs[0].GetFilesystemVersion(), fsversion(ctx, sfs, "@1")))
		require.True(ctx, zfs.FilesystemVersionEqualIdentity(abs[1].GetFilesystemVersion(), fsversion(ctx, sfs, "@2")))
	}

	//
	// end of test setup
	//

	// retry replication with incremental step holds disabled (set to bookmarks-only in this case)
	// - replication should not fail due to holds-related stuff
	// - replication should fail intermittently due to partial sender being fully read
	// - the partial sender is 1/4th the length of the stream, thus expect
	//   successful replication after 5 more attempts
	rep := replicationInvocation{
		sjid:      sjid,
		rjid:      rjid,
		sfs:       sfs,
		rfsRoot:   rfsRoot,
		guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeIncrementalReplication), // !
		interceptSender: func(e *endpoint.Sender) logic.Sender {
			return &PartialSender{Sender: e, failAfterByteCount: 1 << 20}
		},
	}
	rfs := rep.ReceiveSideFilesystem()
	for i := 0; ; i++ {
		require.True(ctx, i < 5)
		report := rep.Do(ctx)
		ctx.Logf("retry run=%v\n%s", i, pretty.Sprint(report))
		_, err := zfs.ZFSGetFilesystemVersion(ctx, rfs+"@2")
		if err == nil {
			break
		}
	}

	// assert replication worked
	fsversion(ctx, rfs, "@2")

	// assert no step holds exist
	abs, absErrs, err := endpoint.ListAbstractions(ctx, endpoint.ListZFSHoldsAndBookmarksQuery{
		FS: endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter{
			FS: &sfs,
		},
		Concurrency: 1,
		JobID:       &sjid,
		What:        endpoint.AbstractionTypeSet{endpoint.AbstractionStepHold: true},
	})
	require.NoError(ctx, err)
	require.Empty(ctx, absErrs)
	require.Len(ctx, abs, 0)

	// assert that the replication cursor bookmark exists
	abs, absErrs, err = endpoint.ListAbstractions(ctx, endpoint.ListZFSHoldsAndBookmarksQuery{
		FS: endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter{
			FS: &sfs,
		},
		Concurrency: 1,
		JobID:       &sjid,
		What:        endpoint.AbstractionTypeSet{endpoint.AbstractionReplicationCursorBookmarkV2: true},
	})
	require.NoError(ctx, err)
	require.Empty(ctx, absErrs)
	require.Len(ctx, abs, 1)
	require.True(ctx, zfs.FilesystemVersionEqualIdentity(abs[0].GetFilesystemVersion(), snap2sfs))
}

func ReplicationStepCompletedLostBehavior__GuaranteeResumability(ctx *platformtest.Context) {
	scenario := replicationStepCompletedLostBehavior_impl(ctx, pdu.ReplicationGuaranteeKind_GuaranteeResumability)

	require.Error(ctx, scenario.deleteSfs1Err, "protected by holds")
	require.Contains(ctx, scenario.deleteSfs1Err.Error(), "dataset is busy")

	require.Error(ctx, scenario.deleteSfs2Err, "protected by holds")
	require.Contains(ctx, scenario.deleteSfs2Err.Error(), "dataset is busy")

	require.Nil(ctx, scenario.finalReport.Error())
	_ = fsversion(ctx, scenario.rfs, "@3") // @3 ade it to the other side
}

func ReplicationStepCompletedLostBehavior__GuaranteeIncrementalReplication(ctx *platformtest.Context) {
	scenario := replicationStepCompletedLostBehavior_impl(ctx, pdu.ReplicationGuaranteeKind_GuaranteeIncrementalReplication)

	require.NoError(ctx, scenario.deleteSfs1Err, "not protected by holds")
	require.NoError(ctx, scenario.deleteSfs2Err, "not protected by holds")

	// step bookmarks should protect against loss of StepCompleted message
	require.Nil(ctx, scenario.finalReport.Error())
	_ = fsversion(ctx, scenario.rfs, "@3") // @3 ade it to the other side
}

type FailSendCompletedSender struct {
	*endpoint.Sender
}

var _ logic.Sender = (*FailSendCompletedSender)(nil)

func (p *FailSendCompletedSender) SendCompleted(ctx context.Context, r *pdu.SendCompletedReq) (*pdu.SendCompletedRes, error) {
	return nil, fmt.Errorf("[mock] SendCompleted not delivered to actual endpoint")
}

type replicationStepCompletedLost_scenario struct {
	rfs                          string
	deleteSfs1Err, deleteSfs2Err error
	finalReport                  *report.FilesystemReport
}

func replicationStepCompletedLostBehavior_impl(ctx *platformtest.Context, guaranteeKind pdu.ReplicationGuaranteeKind) *replicationStepCompletedLost_scenario {

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

	// fully replicate snapshots @1
	{
		mustSnapshot(ctx, sfs+"@1")
		rep := replicationInvocation{
			sjid:      sjid,
			rjid:      rjid,
			sfs:       sfs,
			rfsRoot:   rfsRoot,
			guarantee: pdu.ReplicationConfigProtectionWithKind(guaranteeKind),
		}
		rfs := rep.ReceiveSideFilesystem()
		report := rep.Do(ctx)
		ctx.Logf("\n%s", pretty.Sprint(report))
		// assert this worked (not the main subject of the test)
		_ = fsversion(ctx, rfs, "@1")
	}

	// create a second snapshot @2
	mustSnapshot(ctx, sfs+"@2")

	// fake loss of stepcompleted message
	rep := replicationInvocation{
		sjid:      sjid,
		rjid:      rjid,
		sfs:       sfs,
		rfsRoot:   rfsRoot,
		guarantee: pdu.ReplicationConfigProtectionWithKind(guaranteeKind),
		interceptSender: func(e *endpoint.Sender) logic.Sender {
			return &FailSendCompletedSender{e}
		},
	}
	rfs := rep.ReceiveSideFilesystem()
	report := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(report))

	// assert the replication worked
	_ = fsversion(ctx, rfs, "@2")
	// and that we hold it using a last-received-hold
	abs, absErrs, err := endpoint.ListAbstractions(ctx, endpoint.ListZFSHoldsAndBookmarksQuery{
		FS: endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter{
			FS: &rfs,
		},
		Concurrency: 1,
		JobID:       &rjid,
		What:        endpoint.AbstractionTypeSet{endpoint.AbstractionLastReceivedHold: true},
	})
	require.NoError(ctx, err)
	require.Empty(ctx, absErrs)
	require.Len(ctx, abs, 1)
	require.True(ctx, zfs.FilesystemVersionEqualIdentity(abs[0].GetFilesystemVersion(), fsversion(ctx, rfs, "@2")))

	// now try to delete @2 on the sender, this should work because don't have step holds on it
	deleteSfs2Err := zfs.ZFSDestroy(ctx, sfs+"@2")
	// defer check to caller

	// and create a new snapshot on the sender
	mustSnapshot(ctx, sfs+"@3")

	// now we have: sender @1, @3
	//              recver @1, @2

	// delete @1 on both sides to demonstrate that, if we didn't have bookmarks, we would be out of sync
	deleteSfs1Err := zfs.ZFSDestroy(ctx, sfs+"@1")
	// defer check to caller
	err = zfs.ZFSDestroy(ctx, rfs+"@1")
	require.NoError(ctx, err)

	// attempt replication and return the filesystem report report
	{
		rep := replicationInvocation{
			sjid:      sjid,
			rjid:      rjid,
			sfs:       sfs,
			rfsRoot:   rfsRoot,
			guarantee: pdu.ReplicationConfigProtectionWithKind(guaranteeKind),
		}
		report := rep.Do(ctx)
		ctx.Logf("expecting failure:\n%s", pretty.Sprint(report))
		require.Len(ctx, report.Attempts, 1)
		require.Len(ctx, report.Attempts[0].Filesystems, 1)
		return &replicationStepCompletedLost_scenario{
			rfs:           rfs,
			deleteSfs1Err: deleteSfs1Err,
			deleteSfs2Err: deleteSfs2Err,
			finalReport:   report.Attempts[0].Filesystems[0],
		}
	}

}

type ErroringReceiver struct {
	recvErr error
	*endpoint.Receiver
}

func (r *ErroringReceiver) Receive(ctx context.Context, req *pdu.ReceiveReq, stream io.ReadCloser) (*pdu.ReceiveRes, error) {
	return nil, r.recvErr
}

type NeverEndingSender struct {
	*endpoint.Sender
}

func (s *NeverEndingSender) SendDry(ctx context.Context, req *pdu.SendReq) (r *pdu.SendRes, err error) {
	r, _, err = s.sendImpl(ctx, req, true)
	return r, err
}

func (s *NeverEndingSender) Send(ctx context.Context, req *pdu.SendReq) (r *pdu.SendRes, stream io.ReadCloser, _ error) {
	return s.sendImpl(ctx, req, false)
}

func (s *NeverEndingSender) sendImpl(ctx context.Context, req *pdu.SendReq, dry bool) (r *pdu.SendRes, stream io.ReadCloser, _ error) {
	stream = nil
	r = &pdu.SendRes{
		UsedResumeToken: false,
		ExpectedSize:    1 << 30,
	}
	if dry {
		return r, stream, nil
	}
	dz, err := os.Open("/dev/zero")
	if err != nil {
		panic(err)
	}
	return r, dz, nil
}

func ReplicationReceiverErrorWhileStillSending(ctx *platformtest.Context) {

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

	mockRecvErr := fmt.Errorf("YiezahK3thie8ahKiel5sah2uugei2ize1yi8feivuu7musoat")

	rep := replicationInvocation{
		sjid:      sjid,
		rjid:      rjid,
		sfs:       sfs,
		rfsRoot:   rfsRoot,
		guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeNothing),
		interceptReceiver: func(r *endpoint.Receiver) logic.Receiver {
			return &ErroringReceiver{recvErr: mockRecvErr, Receiver: r}
		},
		interceptSender: func(s *endpoint.Sender) logic.Sender {
			return &NeverEndingSender{s}
		},
	}

	// first replication
	report := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(report))

	require.Len(ctx, report.Attempts, 1)
	attempt := report.Attempts[0]
	require.Nil(ctx, attempt.PlanError)
	require.Len(ctx, attempt.Filesystems, 1)
	afs := attempt.Filesystems[0]
	require.Nil(ctx, afs.PlanError)
	require.Len(ctx, afs.Steps, 1)
	require.Nil(ctx, afs.PlanError)
	require.NotNil(ctx, afs.StepError)
	require.Contains(ctx, afs.StepError.Err, mockRecvErr.Error())
}

func ReplicationFailingInitialParentProhibitsChildReplication(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "sender"
		+  "sender/a"
		+  "sender/a/child"
		+  "sender/aa"
		+  "receiver"
		R  zfs create -p "${ROOTDS}/receiver/${ROOTDS}"
		R  zfs snapshot -r ${ROOTDS}/sender@initial
	`)

	sjid := endpoint.MustMakeJobID("sender-job")
	rjid := endpoint.MustMakeJobID("receiver-job")

	fsA := ctx.RootDataset + "/sender/a"
	fsAChild := ctx.RootDataset + "/sender/a/child"
	fsAA := ctx.RootDataset + "/sender/aa"

	sfilter := filters.NewDatasetMapFilter(3, true)
	mustAddToSFilter(ctx, sfilter, fsA)
	mustAddToSFilter(ctx, sfilter, fsAChild)
	mustAddToSFilter(ctx, sfilter, fsAA)
	rfsRoot := ctx.RootDataset + "/receiver"

	mockRecvErr := fmt.Errorf("yifae4ohPhaquaes0hohghiep9oufie4roo7quoWooluaj2ee8")

	rep := replicationInvocation{
		sjid:      sjid,
		rjid:      rjid,
		sfilter:   sfilter,
		rfsRoot:   rfsRoot,
		guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeNothing),
		interceptReceiver: func(r *endpoint.Receiver) logic.Receiver {
			return &ErroringReceiver{recvErr: mockRecvErr, Receiver: r}
		},
	}

	r := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(r))

	require.Len(ctx, r.Attempts, 1)
	attempt := r.Attempts[0]
	require.Nil(ctx, attempt.PlanError)
	require.Len(ctx, attempt.Filesystems, 3)

	fsByName := make(map[string]*report.FilesystemReport, len(attempt.Filesystems))
	for _, fs := range attempt.Filesystems {
		fsByName[fs.Info.Name] = fs
	}

	require.Contains(ctx, fsByName, fsA)
	require.Contains(ctx, fsByName, fsAChild)
	require.Contains(ctx, fsByName, fsAA)

	checkFS := func(fs string, expectErrMsg string) {
		rep := fsByName[fs]
		require.Len(ctx, rep.Steps, 1)
		require.Nil(ctx, rep.PlanError)
		require.NotNil(ctx, rep.StepError)
		require.Contains(ctx, rep.StepError.Err, expectErrMsg)
	}

	checkFS(fsA, mockRecvErr.Error())
	checkFS(fsAChild, "parent(s) failed during initial replication")
	checkFS(fsAA, mockRecvErr.Error()) // fsAA is not treated as a child of fsA
}

func ReplicationPropertyReplicationWorks(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "sender"
		+  "sender/a"
		+  "sender/a@1"
		+  "sender/a/child"
		+  "sender/a/child@1"
		+  "receiver"
		R  zfs create -p "${ROOTDS}/receiver/${ROOTDS}/sender"
	`)

	sjid := endpoint.MustMakeJobID("sender-job")
	rjid := endpoint.MustMakeJobID("receiver-job")

	fsA := ctx.RootDataset + "/sender/a"
	fsAChild := ctx.RootDataset + "/sender/a/child"

	sfilter := filters.NewDatasetMapFilter(2, true)
	mustAddToSFilter(ctx, sfilter, fsA)
	mustAddToSFilter(ctx, sfilter, fsAChild)
	rfsRoot := ctx.RootDataset + "/receiver"

	type testPropExpectation struct {
		Exists             bool
		Source             zfs.PropertySource
		ExpectSpecialValue string
	}
	type testProp struct {
		Name           string
		SetOnSender    map[string]bool
		ExpectReceiver map[string]testPropExpectation
	}
	testProps := []testProp{
		{
			Name:        "zrepl:ignored",
			SetOnSender: map[string]bool{fsA: true, fsAChild: true},
			ExpectReceiver: map[string]testPropExpectation{
				fsA: {
					Exists: false,
				},
				fsAChild: {
					Exists: false,
				},
			},
		},
		{
			Name:        "zrepl:replicate",
			SetOnSender: map[string]bool{fsA: true, fsAChild: true},
			ExpectReceiver: map[string]testPropExpectation{
				fsA:      {Exists: true, Source: zfs.SourceReceived},
				fsAChild: {Exists: true, Source: zfs.SourceReceived},
			},
		},
		{
			Name:        "zrepl:overridden",
			SetOnSender: map[string]bool{fsA: false, fsAChild: true},
			ExpectReceiver: map[string]testPropExpectation{
				fsA:      {Exists: true, Source: zfs.SourceLocal, ExpectSpecialValue: "overridden value"},
				fsAChild: {Exists: true, Source: zfs.SourceLocal, ExpectSpecialValue: "overridden value"},
			},
		},
	}

	for _, prop := range testProps {
		for fs := range prop.SetOnSender {
			err := zfs.ZFSSet(ctx, mustDatasetPath(fs), map[string]string{prop.Name: prop.Name})
			require.NoError(ctx, err)
		}
	}

	rep := replicationInvocation{
		sjid:      sjid,
		rjid:      rjid,
		sfilter:   sfilter,
		rfsRoot:   rfsRoot,
		guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeNothing),
		receiverConfigHook: func(c *endpoint.ReceiverConfig) {
			c.InheritProperties = []zfsprop.Property{"zrepl:ignored"}
			c.OverrideProperties = map[zfsprop.Property]string{
				"zrepl:overridden": "overridden value",
			}
		},
		senderConfigHook: func(c *endpoint.SenderConfig) {
			c.SendProperties = true // TODO: do another tier with SendBackupProperties
		},
	}

	r := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(r))

	require.Len(ctx, r.Attempts, 1)
	attempt := r.Attempts[0]
	require.Nil(ctx, attempt.PlanError)
	require.Len(ctx, attempt.Filesystems, 2)

	fsByName := make(map[string]*report.FilesystemReport, len(attempt.Filesystems))
	for _, fs := range attempt.Filesystems {
		fsByName[fs.Info.Name] = fs
	}

	require.Contains(ctx, fsByName, fsA)
	require.Contains(ctx, fsByName, fsAChild)
	require.Len(ctx, fsByName, 2)

	requireFSReportSucceeded := func(fs string) *zfs.DatasetPath {
		rep := fsByName[fs]
		require.Len(ctx, rep.Steps, 1)
		require.Nil(ctx, rep.PlanError)
		if rep.StepError != nil && strings.Contains(rep.StepError.Error(), "invalid option 'x'") {
			ctx.SkipNow() // XXX feature detection
		}
		require.Nil(ctx, rep.StepError)
		require.Len(ctx, rep.Steps, 1)
		require.Equal(ctx, 1, rep.CurrentStep)
		return mustDatasetPath(path.Join(rfsRoot, fs))
	}

	rfsA := requireFSReportSucceeded(fsA)
	rfsAChild := requireFSReportSucceeded(fsAChild)

	rfsmap := map[string]*zfs.DatasetPath{
		fsA:      rfsA,
		fsAChild: rfsAChild,
	}

	for fs, rfs := range rfsmap {
		for _, tp := range testProps {
			r_, err := zfs.ZFSGet(ctx, rfs, []string{tp.Name})
			require.NoError(ctx, err)
			r := r_.GetDetails(tp.Name)

			expect := tp.ExpectReceiver[fs]

			if !expect.Exists {
				require.Equal(ctx, zfs.SourceNone, r.Source)
			} else {
				require.Equal(ctx, expect.Source, r.Source)

				if expect.ExpectSpecialValue != "" {
					require.Equal(ctx, expect.ExpectSpecialValue, r.Value)
				} else {
					require.Equal(ctx, tp.Name, r.Value)
				}
			}
		}
	}
}

func ReplicationPlaceholderEncryption__UnspecifiedLeadsToFailureAtRuntimeWhenCreatingPlaceholders(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "sender"
		+  "sender/a"
		+  "sender/a/child"
		+  "receiver"
		R  zfs snapshot -r ${ROOTDS}/sender@initial
	`)

	sjid := endpoint.MustMakeJobID("sender-job")
	rjid := endpoint.MustMakeJobID("receiver-job")

	childfs := ctx.RootDataset + "/sender/a/child"

	sfilter := filters.NewDatasetMapFilter(3, true)

	mustAddToSFilter(ctx, sfilter, childfs)
	rfsRoot := ctx.RootDataset + "/receiver"

	rep := replicationInvocation{
		sjid:      sjid,
		rjid:      rjid,
		sfilter:   sfilter,
		rfsRoot:   rfsRoot,
		guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeResumability),
		receiverConfigHook: func(rc *endpoint.ReceiverConfig) {
			rc.PlaceholderEncryption = endpoint.PlaceholderCreationEncryptionPropertyUnspecified
		},
	}

	r := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(r))

	require.Len(ctx, r.Attempts, 1)
	attempt := r.Attempts[0]
	require.Nil(ctx, attempt.PlanError)
	require.Len(ctx, attempt.Filesystems, 1)

	afs := attempt.Filesystems[0]
	require.Equal(ctx, childfs, afs.Info.Name)

	require.Equal(ctx, 1, len(afs.Steps))
	require.Equal(ctx, 0, afs.CurrentStep)

	require.Equal(ctx, report.FilesystemSteppingErrored, afs.State)

	childfsFirstComponent := strings.Split(childfs, "/")[0]
	require.Contains(ctx, afs.StepError.Err, "cannot create placeholder filesystem "+rfsRoot+"/"+childfsFirstComponent+": placeholder filesystem encryption handling is unspecified in receiver config")
}

type ClientIdentityReceiver struct {
	clientIdentity string
	*endpoint.Receiver
}

func (r *ClientIdentityReceiver) Receive(ctx context.Context, req *pdu.ReceiveReq, stream io.ReadCloser) (*pdu.ReceiveRes, error) {
	ctx = context.WithValue(ctx, endpoint.ClientIdentityKey, r.clientIdentity)
	return r.Receiver.Receive(ctx, req, stream)
}

func ReplicationPlaceholderEncryption__UnspecifiedIsOkForClientIdentityPlaceholder(ctx *platformtest.Context) {
	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "receiver"
	`)

	sjid := endpoint.MustMakeJobID("sender-job")
	rjid := endpoint.MustMakeJobID("receiver-job")

	sfilter := filters.NewDatasetMapFilter(1, true)

	// hacky...
	comps := strings.Split(ctx.RootDataset, "/")
	require.GreaterOrEqual(ctx, len(comps), 2)
	pool := comps[0]
	require.Contains(ctx, pool, "zreplplatformtest", "don't want to cause accidents")
	poolchild := pool + "/" + comps[1]

	err := zfs.ZFSSnapshot(ctx, mustDatasetPath(pool), "testsnap", false)
	require.NoError(ctx, err)

	err = zfs.ZFSSnapshot(ctx, mustDatasetPath(poolchild), "testsnap", false)
	require.NoError(ctx, err)

	mustAddToSFilter(ctx, sfilter, pool)
	mustAddToSFilter(ctx, sfilter, poolchild)

	clientIdentity := "testclientid"

	rfsRoot := ctx.RootDataset + "/receiver"

	rep := replicationInvocation{
		sjid:      sjid,
		rjid:      rjid,
		sfilter:   sfilter,
		rfsRoot:   rfsRoot,
		guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeResumability),
		receiverConfigHook: func(rc *endpoint.ReceiverConfig) {
			rc.PlaceholderEncryption = endpoint.PlaceholderCreationEncryptionPropertyUnspecified
			rc.AppendClientIdentity = true
		},
		interceptReceiver: func(r *endpoint.Receiver) logic.Receiver {
			r.Test_OverrideClientIdentityFunc = func() string { return clientIdentity }
			return r
		},
	}

	r := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(r))

	require.Len(ctx, r.Attempts, 1)
	attempt := r.Attempts[0]
	require.Nil(ctx, attempt.PlanError)
	require.Len(ctx, attempt.Filesystems, 2)

	filesystemsByName := make(map[string]*report.FilesystemReport)
	for _, fs := range attempt.Filesystems {
		filesystemsByName[fs.Info.Name] = fs
	}
	require.Len(ctx, filesystemsByName, len(attempt.Filesystems))

	afs, ok := filesystemsByName[pool]
	require.True(ctx, ok)
	require.Nil(ctx, afs.PlanError)
	require.Nil(ctx, afs.StepError)
	require.Equal(ctx, report.FilesystemDone, afs.State)

	afs, ok = filesystemsByName[poolchild]
	require.True(ctx, ok)
	require.Nil(ctx, afs.PlanError)
	require.Nil(ctx, afs.StepError)
	require.Equal(ctx, report.FilesystemDone, afs.State)

	mustGetFilesystemVersion(ctx, rfsRoot+"/"+clientIdentity+"/"+pool+"@testsnap")
	mustGetFilesystemVersion(ctx, rfsRoot+"/"+clientIdentity+"/"+poolchild+"@testsnap")

}

func replicationPlaceholderEncryption__EncryptOnReceiverUseCase__impl(ctx *platformtest.Context, placeholderEncryption endpoint.PlaceholderCreationEncryptionProperty) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "sender"
		+  "sender/a"
		+  "sender/a/child"
		+  "receiver" encrypted
		R  zfs snapshot -r ${ROOTDS}/sender@initial
	`)

	sjid := endpoint.MustMakeJobID("sender-job")
	rjid := endpoint.MustMakeJobID("receiver-job")

	childfs := ctx.RootDataset + "/sender/a/child"

	sfilter := filters.NewDatasetMapFilter(3, true)

	mustAddToSFilter(ctx, sfilter, childfs)
	rfsRoot := ctx.RootDataset + "/receiver"

	rep := replicationInvocation{
		sjid:      sjid,
		rjid:      rjid,
		sfilter:   sfilter,
		rfsRoot:   rfsRoot,
		guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeResumability),
		receiverConfigHook: func(rc *endpoint.ReceiverConfig) {
			rc.PlaceholderEncryption = placeholderEncryption
			rc.AppendClientIdentity = false
		},
	}

	r := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(r))

	require.Len(ctx, r.Attempts, 1)
	attempt := r.Attempts[0]
	require.Equal(ctx, report.AttemptDone, attempt.State)
	require.Len(ctx, attempt.Filesystems, 1)
	afs := attempt.Filesystems[0]
	require.Equal(ctx, childfs, afs.Info.Name)

	require.Equal(ctx, 1, len(afs.Steps))

	rfs := mustDatasetPath(rfsRoot + "/" + childfs)
	mustGetFilesystemVersion(ctx, rfs.ToString()+"@initial")
}

func ReplicationPlaceholderEncryption__EncryptOnReceiverUseCase__WorksIfConfiguredWithInherit(ctx *platformtest.Context) {
	placeholderEncryption := endpoint.PlaceholderCreationEncryptionPropertyInherit

	replicationPlaceholderEncryption__EncryptOnReceiverUseCase__impl(ctx, placeholderEncryption)
	childfs := ctx.RootDataset + "/sender/a/child"
	rfsRoot := ctx.RootDataset + "/receiver"
	rfs := mustDatasetPath(rfsRoot + "/" + childfs)

	// The leaf child dataset should be inhering from rfsRoot.
	// If we had replicated with PlaceholderCreationEncryptionPropertyOff
	// then it would be unencrypted and inherit from the placeholder.
	props, err := zfs.ZFSGet(ctx, rfs, []string{"encryptionroot"})
	require.NoError(ctx, err)
	require.Equal(ctx, rfsRoot, props.Get("encryptionroot"))
}

func replicationInitialImpl(ctx *platformtest.Context, iras logic.InitialReplicationAutoResolution, expectExactRfsSnaps []string) *report.Report {
	// reverse order for snap names to expose sorting assumptions
	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		CREATEROOT
		+  "sender"
		+  "sender@3"
		+  "sender@2"
		+  "sender@1"
		+  "receiver"
		R  zfs create -p "${ROOTDS}/receiver/${ROOTDS}"
	`)

	sjid := endpoint.MustMakeJobID("sender-job")
	rjid := endpoint.MustMakeJobID("receiver-job")

	sfs := ctx.RootDataset + "/sender"
	rfsRoot := ctx.RootDataset + "/receiver"

	rep := replicationInvocation{
		sjid:      sjid,
		rjid:      rjid,
		sfs:       sfs,
		rfsRoot:   rfsRoot,
		guarantee: pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeResumability),
		plannerPolicyHook: func(pp *logic.PlannerPolicy) {
			pp.ConflictResolution.InitialReplication = iras
		},
	}
	rfs := rep.ReceiveSideFilesystem()

	// first replication
	report := rep.Do(ctx)
	ctx.Logf("\n%s", pretty.Sprint(report))

	versions, err := zfs.ZFSListFilesystemVersions(ctx, mustDatasetPath(rfs), zfs.ListFilesystemVersionsOptions{Types: zfs.Snapshots})
	if _, ok := err.(*zfs.DatasetDoesNotExist); ok {
		versions = nil
	} else {
		require.NoError(ctx, err)
	}

	bySnapName := make(map[string]int)
	for _, v := range versions {
		bySnapName[v.GetName()] += 1
	}
	for _, v := range expectExactRfsSnaps {
		bySnapName[v] -= 1
	}

	for _, v := range bySnapName {
		if v != 0 {
			ctx.Logf("unexpected snaps:\n%#v", bySnapName)
			ctx.FailNow()
		}
	}

	return report
}

func ReplicationInitialAll(ctx *platformtest.Context) {
	replicationInitialImpl(ctx, logic.InitialReplicationAutoResolutionAll, []string{"3", "2", "1"})
}

func ReplicationInitialMostRecent(ctx *platformtest.Context) {
	replicationInitialImpl(ctx, logic.InitialReplicationAutoResolutionMostRecent, []string{"1"})
}

func ReplicationInitialFail(ctx *platformtest.Context) {
	report := replicationInitialImpl(ctx, logic.InitialReplicationAutoResolutionFail, []string{})
	require.Len(ctx, report.Attempts, 1)
	require.Nil(ctx, report.Attempts[0].PlanError)
	require.Len(ctx, report.Attempts[0].Filesystems, 1)
	require.NotNil(ctx, report.Attempts[0].Filesystems[0].PlanError)
	require.Contains(ctx, report.Attempts[0].Filesystems[0].PlanError.Err, "automatic conflict resolution for initial replication is disabled in config")
}
