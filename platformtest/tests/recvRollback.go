package tests

import (
	"fmt"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/util/nodefault"
	"github.com/zrepl/zrepl/zfs"
)

func ReceiveForceRollbackWorksUnencrypted(ctx *platformtest.Context) {
	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "foo bar"
		+  "foo bar@a snap"
		+  "foo bar@another snap"
		+  "foo bar@snap3"
		+  "sender"
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
	require.NoError(ctx, err)

	// assert exists on receiver
	rfsSnap1 := fsversion(ctx, rfs, "@1")
	// assert it's the only one (rollback and force-recv should be blowing away the other filesystems)
	rfsVersions, err := zfs.ZFSListFilesystemVersions(ctx, mustDatasetPath(rfs), zfs.ListFilesystemVersionsOptions{})
	require.NoError(ctx, err)
	assert.Len(ctx, rfsVersions, 1)
	assert.Equal(ctx, rfsVersions[0], rfsSnap1)
}
