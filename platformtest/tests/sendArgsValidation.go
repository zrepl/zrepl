package tests

import (
	"fmt"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/util/nodefault"
	"github.com/zrepl/zrepl/zfs"
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
