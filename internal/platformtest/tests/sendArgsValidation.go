package tests

import (
	"fmt"
	"path"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/require"

	"github.com/LyingCak3/zrepl/internal/endpoint"
	"github.com/LyingCak3/zrepl/internal/platformtest"
	"github.com/LyingCak3/zrepl/internal/replication/logic"
	"github.com/LyingCak3/zrepl/internal/replication/logic/pdu"
	"github.com/LyingCak3/zrepl/internal/replication/report"
	"github.com/LyingCak3/zrepl/internal/util/nodefault"
	"github.com/LyingCak3/zrepl/internal/zfs"
)

func SendArgsValidationEncryptedSendOfUnencryptedDatasetForbidden__EncryptionSupported_true(ctx *platformtest.Context) {
	sendArgsValidationEncryptedSendOfUnencryptedDatasetForbidden_impl(ctx, true)
}

func SendArgsValidationEncryptedSendOfUnencryptedDatasetForbidden__EncryptionSupported_false(ctx *platformtest.Context) {
	sendArgsValidationEncryptedSendOfUnencryptedDatasetForbidden_impl(ctx, false)
}

func sendArgsValidationEncryptedSendOfUnencryptedDatasetForbidden_impl(ctx *platformtest.Context, testForEncryptionSupported bool) {

	supported, err := zfs.EncryptionCLISupported(ctx)
	check(err)
	if supported != testForEncryptionSupported {
		ctx.SkipNow()
	}
	noEncryptionCLISupport := !supported

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
	DESTROYROOT
	CREATEROOT
	+	"send er"
	+   "send er@a snap"
	`)

	fs := fmt.Sprintf("%s/send er", ctx.RootDataset)
	props := mustGetFilesystemVersion(ctx, fs+"@a snap")

	sendArgs, err := zfs.ZFSSendArgsUnvalidated{
		FS: fs,
		To: &zfs.ZFSSendArgVersion{
			RelName: "@a snap",
			GUID:    props.Guid,
		},
		ZFSSendFlags: zfs.ZFSSendFlags{
			Encrypted:   &nodefault.Bool{B: true},
			ResumeToken: "",
		},
	}.Validate(ctx)

	var stream *zfs.SendStream
	if err == nil {
		stream, err = zfs.ZFSSend(ctx, sendArgs) // no shadow
		if err == nil {
			defer stream.Close()
		}
		// fallthrough
	}

	if noEncryptionCLISupport {
		require.Error(ctx, err)
		saverr, ok := err.(*zfs.ZFSSendArgsValidationError)
		require.True(ctx, ok, "%T", err)
		require.Equal(ctx, zfs.ZFSSendArgsEncryptedSendRequestedButFSUnencrypted, saverr.What)
		return
	}
	require.Error(ctx, err)
	ctx.Logf("send err: %T %s", err, err)
	validationErr, ok := err.(*zfs.ZFSSendArgsValidationError)
	require.True(ctx, ok)
	require.True(ctx, validationErr.What == zfs.ZFSSendArgsEncryptedSendRequestedButFSUnencrypted)
}

func SendArgsValidationResumeTokenEncryptionMismatchForbidden(ctx *platformtest.Context) {

	supported, err := zfs.EncryptionCLISupported(ctx)
	check(err)
	if !supported {
		ctx.SkipNow()
	}
	supported, err = zfs.ResumeSendSupported(ctx)
	check(err)
	if !supported {
		ctx.SkipNow()
	}

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
	DESTROYROOT
	CREATEROOT
	+	"send er" encrypted
	`)

	sendFS := fmt.Sprintf("%s/send er", ctx.RootDataset)
	unencRecvFS := fmt.Sprintf("%s/unenc recv", ctx.RootDataset)
	encRecvFS := fmt.Sprintf("%s/enc recv", ctx.RootDataset)

	src := makeDummyDataSnapshots(ctx, sendFS)

	unencS := makeResumeSituation(ctx, src, unencRecvFS, zfs.ZFSSendArgsUnvalidated{
		FS:           sendFS,
		To:           src.snapA,
		ZFSSendFlags: zfs.ZFSSendFlags{Encrypted: &nodefault.Bool{B: false}}, // !
	}, zfs.RecvOptions{
		RollbackAndForceRecv: false,
		SavePartialRecvState: true,
	})

	encS := makeResumeSituation(ctx, src, encRecvFS, zfs.ZFSSendArgsUnvalidated{
		FS:           sendFS,
		To:           src.snapA,
		ZFSSendFlags: zfs.ZFSSendFlags{Encrypted: &nodefault.Bool{B: true}}, // !
	}, zfs.RecvOptions{
		RollbackAndForceRecv: false,
		SavePartialRecvState: true,
	})

	// threat model: use of a crafted resume token that requests an unencrypted send
	//               but send args require encrypted send
	{
		var maliciousSend zfs.ZFSSendArgsUnvalidated = encS.sendArgs
		maliciousSend.ResumeToken = unencS.recvErrDecoded.ResumeTokenRaw

		_, err := maliciousSend.Validate(ctx)
		validationErr, ok := err.(*zfs.ZFSSendArgsValidationError)
		require.True(ctx, ok)
		require.Equal(ctx, validationErr.What, zfs.ZFSSendArgsResumeTokenMismatch)
		ctx.Logf("%s", validationErr)

		mismatchError, ok := validationErr.Msg.(*zfs.ZFSSendArgsResumeTokenMismatchError)
		require.True(ctx, ok)
		require.Equal(ctx, mismatchError.What, zfs.ZFSSendArgsResumeTokenMismatchEncryptionNotSet)
	}

	// threat model: use of a crafted resume token that requests an encrypted send
	//               but send args require unencrypted send
	{
		var maliciousSend zfs.ZFSSendArgsUnvalidated = unencS.sendArgs
		maliciousSend.ResumeToken = encS.recvErrDecoded.ResumeTokenRaw

		_, err := maliciousSend.Validate(ctx)
		require.Error(ctx, err)
		ctx.Logf("send err: %T %s", err, err)
		validationErr, ok := err.(*zfs.ZFSSendArgsValidationError)
		require.True(ctx, ok)
		require.Equal(ctx, validationErr.What, zfs.ZFSSendArgsResumeTokenMismatch)
		ctx.Logf("%s", validationErr)

		mismatchError, ok := validationErr.Msg.(*zfs.ZFSSendArgsResumeTokenMismatchError)
		require.True(ctx, ok)
		require.Equal(ctx, mismatchError.What, zfs.ZFSSendArgsResumeTokenMismatchEncryptionSet)
	}

}

func SendArgsValidationResumeTokenDifferentFilesystemForbidden(ctx *platformtest.Context) {
	supported, err := zfs.ResumeSendSupported(ctx)
	check(err)
	if !supported {
		ctx.SkipNow()
	}

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
	DESTROYROOT
	CREATEROOT
	+	"send er1"
	+	"send er2"
	`)

	sendFS1 := fmt.Sprintf("%s/send er1", ctx.RootDataset)
	sendFS2 := fmt.Sprintf("%s/send er2", ctx.RootDataset)
	recvFS := fmt.Sprintf("%s/unenc recv", ctx.RootDataset)

	src1 := makeDummyDataSnapshots(ctx, sendFS1)
	src2 := makeDummyDataSnapshots(ctx, sendFS2)

	rs := makeResumeSituation(ctx, src1, recvFS, zfs.ZFSSendArgsUnvalidated{
		FS:           sendFS1,
		To:           src1.snapA,
		ZFSSendFlags: zfs.ZFSSendFlags{Encrypted: &nodefault.Bool{B: false}},
	}, zfs.RecvOptions{
		RollbackAndForceRecv: false,
		SavePartialRecvState: true,
	})

	// threat model: forged resume token tries to steal a full send of snapA on fs2 by
	//               presenting a resume token for full send of snapA on fs1
	var maliciousSend zfs.ZFSSendArgsUnvalidated = zfs.ZFSSendArgsUnvalidated{
		FS: sendFS2,
		To: &zfs.ZFSSendArgVersion{
			RelName: src2.snapA.RelName,
			GUID:    src2.snapA.GUID,
		},
		ZFSSendFlags: zfs.ZFSSendFlags{
			Encrypted:   &nodefault.Bool{B: false},
			ResumeToken: rs.recvErrDecoded.ResumeTokenRaw,
		},
	}
	_, err = maliciousSend.Validate(ctx)
	require.Error(ctx, err)
	ctx.Logf("send err: %T %s", err, err)
	validationErr, ok := err.(*zfs.ZFSSendArgsValidationError)
	require.True(ctx, ok)
	require.Equal(ctx, validationErr.What, zfs.ZFSSendArgsResumeTokenMismatch)
	ctx.Logf("%s", validationErr)

	mismatchError, ok := validationErr.Msg.(*zfs.ZFSSendArgsResumeTokenMismatchError)
	require.True(ctx, ok)
	require.Equal(ctx, mismatchError.What, zfs.ZFSSendArgsResumeTokenMismatchFilesystem)
}

type sendArgsValidationEndToEndTestOutcome string

const (
	ValidationAccepts sendArgsValidationEndToEndTestOutcome = "accept"
	ValidationRejects sendArgsValidationEndToEndTestOutcome = "rejects"
)

type sendArgsValidationEndToEndTest struct {
	encryptedSenderFilesystem             bool
	senderConfigHook                      func(config *endpoint.SenderConfig)
	expectedOutcome                       sendArgsValidationEndToEndTestOutcome
	outcomeRejectsInspectError            func(require.TestingT, *report.FilesystemReport, bool)
	inspectReceiverFSAfterSuccessfulCycle func(rfs string)
}

func implSendArgsValidationEndToEndTest(ctx *platformtest.Context, setup sendArgsValidationEndToEndTest) {

	senderEncrypted := ""
	if setup.encryptedSenderFilesystem {
		senderEncrypted = "encrypted"
	}

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, fmt.Sprintf(`
		CREATEROOT
		+  "sender" %s
		+  "receiver"
		R  zfs create -p "${ROOTDS}/receiver/${ROOTDS}"
	`, senderEncrypted))

	sjid := endpoint.MustMakeJobID("sender-job")
	rjid := endpoint.MustMakeJobID("receiver-job")

	sfs := ctx.RootDataset + "/sender"
	rfsRoot := ctx.RootDataset + "/receiver"

	sfsmp, err := zfs.ZFSGetMountpoint(ctx, sfs)
	require.NoError(ctx, err)
	require.True(ctx, sfsmp.Mounted)

	// Two cycles. one initial replication, one incremental replication.
	// Within each cycle: interrupt replication at least once.
	// This exercises both the no-resume-token-present and the resume-token-present validation code paths.
initial_then_incremental:
	for i := 0; i < 2; i++ {
		writeDummyData(path.Join(sfsmp.Mountpoint, "dummy.data"), 2*(1<<20))
		mustSnapshot(ctx, fmt.Sprintf("%s@%d", sfs, i))

		rep := replicationInvocation{
			sjid:             sjid,
			rjid:             rjid,
			sfs:              sfs,
			rfsRoot:          rfsRoot,
			senderConfigHook: setup.senderConfigHook,
			interceptSender: func(e *endpoint.Sender) logic.Sender {
				return &PartialSender{Sender: e, failAfterByteCount: 1 << 20}
			},
			guarantee:              pdu.ReplicationConfigProtectionWithKind(pdu.ReplicationGuaranteeKind_GuaranteeResumability),
			skipSendArgsValidation: false,
		}

		rfs := rep.ReceiveSideFilesystem()

		// PartialSender interrupts after 1MiB, and we wrote 2 MiB of data
		// => Give it 3 attempts to replicate. after that, we should have a stable outcome
		var lastReport *report.Report
		lastResumeToken := ""
	interrupt_current_step:
		for j := 0; j < 3; j++ {

			lastReport = rep.Do(ctx)
			ctx.Logf("\nreport=%s", pretty.Sprint(lastReport))
			require.Len(ctx, lastReport.Attempts, 1)
			require.Len(ctx, lastReport.Attempts[0].Filesystems, 1)
			lastReportFS := lastReport.Attempts[0].Filesystems[0]

			var rfsExists bool
			rfsResumeToken, err := zfs.ZFSGetReceiveResumeTokenOrEmptyStringIfNotSupported(ctx, mustDatasetPath(rfs))
			if err != nil {
				_, ok := err.(*zfs.DatasetDoesNotExist) // no shadow
				require.True(ctx, ok, "no other errors expected")
				rfsExists = false
				rfsResumeToken = ""
			} else {
				rfsExists = true
			}

			if setup.expectedOutcome == ValidationRejects {

				// When expecting rejection, it should manifest immediately, before sending anything.
				// This is tested in the j=0 iteration (for both initial and incremental repl (i=0, i=1)).
				// But we also want to assert correct behavior in case zrepl observes resume tokens.
				// Specifically, cases where the send parameters encoded in the token conflict with the
				// configured encryption policy.
				// Hence, for scenarios that are expected to reject, after we validated that they reject
				// for the non-resuming case (j==0), fabricate a resuming scenario by temporarily disabling
				// send args validation. After fabricating the scenario, proceed into j==1 to exercise
				// the resume token validation.
				if j == 0 {
					if i == 0 {
						require.False(ctx, rfsExists, "the sender should not have sent anything")
					} else {
						// we fabricate a scenario where rfsExists below, hence can't assert non-existence anymore
					}

					ctx.Logf("skipping send args validation to test resuming case")

					rep.skipSendArgsValidation = true
					setupResumeReport := rep.Do(ctx)
					ctx.Logf("setupResumeReport=%s", pretty.Sprint(setupResumeReport))
					rep.skipSendArgsValidation = false
					rt, err := zfs.ZFSGetReceiveResumeTokenOrEmptyStringIfNotSupported(ctx, mustDatasetPath(rfs))
					require.NoError(ctx, err)
					require.NotEmpty(ctx, rt, "we disabled send args validation, so the .Do above should have resulted in a resume token on rfs")
					lastResumeToken = rt
					continue interrupt_current_step // next iteration will test resume case with send args validation enabled
				} else { // j > 0
					require.Equal(ctx, lastResumeToken, rfsResumeToken, "we expect policy to refuse replication, no progress must happen")
					_, err := zfs.ZFSGetFilesystemVersion(ctx, fmt.Sprintf("%s@%d", rfs, i))
					_, ok := err.(*zfs.DatasetDoesNotExist)
					require.True(ctx, ok, "another check that no progress is happening")
				}

				setup.outcomeRejectsInspectError(ctx, lastReportFS, j == 0)

				// XXX: check rejection cases for incremental replication as well
				break initial_then_incremental

			} else {
				require.Equal(ctx, ValidationAccepts, setup.expectedOutcome)
				_, err := zfs.ZFSGetFilesystemVersion(ctx, fmt.Sprintf("%s@%d", rfs, i))
				_, notExist := err.(*zfs.DatasetDoesNotExist)
				if notExist {
					require.NotEmpty(ctx, rfsResumeToken)
					continue interrupt_current_step // next iteration will resume
				} else {
					require.NoError(ctx, err)
					// version exists

					// make sure all the filesystem versions we created so far were replicated by the replication loop
					for j := 0; j <= i; j++ {
						_ = fsversion(ctx, rfs, fmt.Sprintf("@%d", j))
					}

					setup.inspectReceiverFSAfterSuccessfulCycle(rfs)
					continue initial_then_incremental
				}
			}
		}

	}

}

func SendArgsValidationEE_EncryptionAndRaw(ctx *platformtest.Context) {
	type TC struct {
		// create sender filesystem with encryption enabled yes/no
		SFSEnc              bool
		SndEnc              bool // send flag
		SndRaw              bool // send flag
		RFSEnc              bool
		Outcome             sendArgsValidationEndToEndTestOutcome
		RejectErrorNoResume string
		RejectErrorResume   string
	}
	tcs := []TC{
		// Sender FS is unencrypted
		{SFSEnc: false, SndEnc: false, SndRaw: false, RFSEnc: false, Outcome: ValidationAccepts},
		{SFSEnc: false, SndEnc: false, SndRaw: true, RFSEnc: false, Outcome: ValidationAccepts}, // allow unencrypted raw sends (#503)
		{SFSEnc: false, SndEnc: true, SndRaw: false, RFSEnc: false, Outcome: ValidationRejects,
			RejectErrorNoResume: `encrypted send mandated by policy, but filesystem .* is not encrypted`,
			RejectErrorResume:   `encrypted send mandated by policy, but filesystem .* is not encrypted`,
		},
		{SFSEnc: false, SndEnc: true, SndRaw: true, RFSEnc: false, Outcome: ValidationRejects,
			RejectErrorNoResume: `encrypted send mandated by policy, but filesystem .* is not encrypted`,
			RejectErrorResume:   `encrypted send mandated by policy, but filesystem .* is not encrypted`,
		},
		// Sender FS is encrypted
		{SFSEnc: true, SndEnc: false, SndRaw: false, RFSEnc: false, Outcome: ValidationAccepts}, // passes because keys are loaded, thus can send plain.
		{SFSEnc: true, SndEnc: false, SndRaw: true, RFSEnc: false, Outcome: ValidationRejects,
			RejectErrorNoResume: `policy mandates raw\+unencrypted sends, but filesystem .* is encrypted`,
			RejectErrorResume:   `resume token has rawok=true which would result in encrypted send, but policy mandates unencrypted sends only`,
		},
		{SFSEnc: true, SndEnc: true, SndRaw: false, RFSEnc: true, Outcome: ValidationAccepts},
		{SFSEnc: true, SndEnc: true, SndRaw: true, RFSEnc: true, Outcome: ValidationAccepts},
	}

	for _, tc := range tcs {
		tc := tc // closure would copy by ref otherwise
		ctx.QueueSubtest(fmt.Sprintf("%#v", tc), func(ctx *platformtest.Context) {
			implSendArgsValidationEndToEndTest(ctx, sendArgsValidationEndToEndTest{
				encryptedSenderFilesystem: tc.SFSEnc,
				senderConfigHook: func(c *endpoint.SenderConfig) {
					c.Encrypt = &nodefault.Bool{B: tc.SndEnc}
					c.SendRaw = tc.SndRaw
				},
				expectedOutcome: tc.Outcome,
				outcomeRejectsInspectError: func(ctx require.TestingT, fr *report.FilesystemReport, isResume bool) {
					// this callback is only called for ValidationRejects

					// validation should be failing during dry send => planning stage
					// XXX mock out ZFS to ensure we never call a zfs send that would send data
					// if we're expecting validation to fail
					require.Equal(ctx, report.FilesystemPlanningErrored, fr.State)

					if isResume {
						require.NotEmpty(ctx, tc.RejectErrorResume)
						require.Regexp(ctx, tc.RejectErrorResume, fr.PlanError)
					} else {
						require.NotEmpty(ctx, tc.RejectErrorNoResume)
						require.Regexp(ctx, tc.RejectErrorNoResume, fr.PlanError)
					}
				},
				inspectReceiverFSAfterSuccessfulCycle: func(rfs string) {
					enabled, err := zfs.ZFSGetEncryptionEnabled(ctx, rfs)
					require.NoError(ctx, err)
					require.Equal(ctx, tc.RFSEnc, enabled, "receiver filesystem encryption settings unexpected")
				},
			})
		})
	}
}
