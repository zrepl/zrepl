package tests

import (
	"fmt"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

func ReceiveForceIntoEncryptedErr(ctx *platformtest.Context) {
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
		FS:          sfs,
		Encrypted:   &zfs.NilBool{B: false},
		From:        nil,
		To:          &sfsSnap1,
		ResumeToken: "",
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
