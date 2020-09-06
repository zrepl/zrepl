package tests

import (
	"fmt"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/util/nodefault"
	"github.com/zrepl/zrepl/zfs"
)

func ResumableRecvAndTokenHandling(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
	DESTROYROOT
	CREATEROOT
	+	"send er"
	`)

	sendFS := fmt.Sprintf("%s/send er", ctx.RootDataset)
	recvFS := fmt.Sprintf("%s/recv er", ctx.RootDataset)

	supported, err := zfs.ResumeRecvSupported(ctx, mustDatasetPath(sendFS))
	check(err)

	src := makeDummyDataSnapshots(ctx, sendFS)

	s := makeResumeSituation(ctx, src, recvFS, zfs.ZFSSendArgsUnvalidated{
		FS: sendFS,
		To: src.snapA,
		ZFSSendFlags: zfs.ZFSSendFlags{
			Encrypted:   &nodefault.Bool{B: false},
			ResumeToken: "",
		},
	}, zfs.RecvOptions{
		RollbackAndForceRecv: false, // doesnt' exist yet
		SavePartialRecvState: true,
	})

	if !supported {
		_, ok := s.recvErr.(*zfs.ErrRecvResumeNotSupported)
		require.True(ctx, ok)

		// we know that support on sendFS implies support on recvFS
		// => assert that if we don't support resumed recv, the method returns ""
		tok, err := zfs.ZFSGetReceiveResumeTokenOrEmptyStringIfNotSupported(ctx, mustDatasetPath(recvFS))
		check(err)
		require.Equal(ctx, "", tok)

		return // nothing more to test for recv that doesn't support -s
	}

	getTokenRaw, err := zfs.ZFSGetReceiveResumeTokenOrEmptyStringIfNotSupported(ctx, mustDatasetPath(recvFS))
	check(err)

	require.NotEmpty(ctx, getTokenRaw)
	decodedToken, err := zfs.ParseResumeToken(ctx, getTokenRaw)
	check(err)

	require.True(ctx, decodedToken.HasToGUID)
	require.Equal(ctx, s.sendArgs.To.GUID, decodedToken.ToGUID)

	recvErr := s.recvErr.(*zfs.RecvFailedWithResumeTokenErr)
	require.Equal(ctx, recvErr.ResumeTokenRaw, getTokenRaw)
	require.Equal(ctx, recvErr.ResumeTokenParsed, decodedToken)
}
