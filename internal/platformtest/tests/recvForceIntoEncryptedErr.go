package tests

import (
	"fmt"

	"github.com/stretchr/testify/require"

	"github.com/LyingCak3/zrepl/internal/platformtest"
	"github.com/LyingCak3/zrepl/internal/util/nodefault"
	"github.com/LyingCak3/zrepl/internal/zfs"
)

func ReceiveForceIntoEncryptedErr(ctx *platformtest.Context) {

	supported, err := zfs.EncryptionCLISupported(ctx)
	require.NoError(ctx, err, "encryption feature test failed")
	if !supported {
		ctx.SkipNow()
		return
	}

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "foo bar" encrypted
		+  "sender" encrypted
		+  "sender@1"
	`)

	rfs := fmt.Sprintf("%s/foo bar", ctx.RootDataset)
	sfs := fmt.Sprintf("%s/sender", ctx.RootDataset)
	sfsSnap1 := sendArgVersion(ctx, sfs, "@1")

	sendArgs, err := zfs.ZFSSendArgsUnvalidated{
		FS:   sfs,
		From: nil,
		To:   &sfsSnap1,
		ZFSSendFlags: zfs.ZFSSendFlags{
			Encrypted:   &nodefault.Bool{B: false},
			ResumeToken: "",
		},
	}.Validate(ctx)
	require.NoError(ctx, err)

	sendStream, err := zfs.ZFSSend(ctx, sendArgs)
	require.NoError(ctx, err)
	defer sendStream.Close()

	recvOpts := zfs.RecvOptions{
		RollbackAndForceRecv: true,
		SavePartialRecvState: false,
	}
	err = zfs.ZFSRecv(ctx, rfs, &zfs.ZFSSendArgVersion{RelName: "@1", GUID: sfsSnap1.GUID}, sendStream, recvOpts)
	require.Error(ctx, err)
	re, ok := err.(*zfs.RecvDestroyOrOverwriteEncryptedErr)
	require.True(ctx, ok)
	require.Contains(ctx, re.Error(), "zfs receive -F cannot be used to destroy an encrypted filesystem or overwrite an unencrypted one with an encrypted on")
}
