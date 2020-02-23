package tests

import (
	"io"
	"math/rand"
	"os"
	"path"
	"strings"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/util/limitio"
	"github.com/zrepl/zrepl/zfs"
)

func sendArgVersion(fs, relName string) zfs.ZFSSendArgVersion {
	guid, err := zfs.ZFSGetGUID(fs, relName)
	if err != nil {
		panic(err)
	}
	return zfs.ZFSSendArgVersion{
		RelName: relName,
		GUID:    guid,
	}
}

func mustDatasetPath(fs string) *zfs.DatasetPath {
	p, err := zfs.NewDatasetPath(fs)
	if err != nil {
		panic(err)
	}
	return p
}

func mustSnapshot(snap string) {
	if err := zfs.EntityNamecheck(snap, zfs.EntityTypeSnapshot); err != nil {
		panic(err)
	}
	comps := strings.Split(snap, "@")
	if len(comps) != 2 {
		panic(comps)
	}
	err := zfs.ZFSSnapshot(mustDatasetPath(comps[0]), comps[1], false)
	if err != nil {
		panic(err)
	}
}

func mustGetProps(entity string) zfs.ZFSPropCreateTxgAndGuidProps {
	props, err := zfs.ZFSGetCreateTXGAndGuid(entity)
	check(err)
	return props
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

var dummyDataRand = rand.New(rand.NewSource(99))

func writeDummyData(path string, numBytes int64) {
	r := io.LimitReader(dummyDataRand, numBytes)
	d, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	check(err)
	defer d.Close()
	_, err = io.Copy(d, r)
	check(err)
}

type dummySnapshotSituation struct {
	sendFS       string
	dummyDataLen int64
	snapA        *zfs.ZFSSendArgVersion
	snapB        *zfs.ZFSSendArgVersion
}

type resumeSituation struct {
	sendArgs         zfs.ZFSSendArgs
	recvOpts         zfs.RecvOptions
	sendErr, recvErr error
	recvErrDecoded   *zfs.RecvFailedWithResumeTokenErr
}

func makeDummyDataSnapshots(ctx *platformtest.Context, sendFS string) (situation dummySnapshotSituation) {

	situation.sendFS = sendFS
	sendFSMount, err := zfs.ZFSGetMountpoint(sendFS)
	require.NoError(ctx, err)
	require.True(ctx, sendFSMount.Mounted)

	const dummyLen = int64(10 * (1 << 20))
	situation.dummyDataLen = dummyLen

	writeDummyData(path.Join(sendFSMount.Mountpoint, "dummy_data"), dummyLen)
	mustSnapshot(sendFS + "@a snapshot")
	snapA := sendArgVersion(sendFS, "@a snapshot")
	situation.snapA = &snapA

	writeDummyData(path.Join(sendFSMount.Mountpoint, "dummy_data"), dummyLen)
	mustSnapshot(sendFS + "@b snapshot")
	snapB := sendArgVersion(sendFS, "@b snapshot")
	situation.snapB = &snapB

	return situation
}

func makeResumeSituation(ctx *platformtest.Context, src dummySnapshotSituation, recvFS string, sendArgs zfs.ZFSSendArgs, recvOptions zfs.RecvOptions) *resumeSituation {

	situation := &resumeSituation{}

	situation.sendArgs = sendArgs
	situation.recvOpts = recvOptions
	require.True(ctx, recvOptions.SavePartialRecvState, "this method would be pointless otherwise")
	require.Equal(ctx, sendArgs.FS, src.sendFS)

	copier, err := zfs.ZFSSend(ctx, sendArgs)
	situation.sendErr = err
	if err != nil {
		return situation
	}

	limitedCopier := zfs.NewReadCloserCopier(limitio.ReadCloser(copier, src.dummyDataLen/2))
	defer limitedCopier.Close()

	require.NotNil(ctx, sendArgs.To)
	err = zfs.ZFSRecv(ctx, recvFS, sendArgs.To, limitedCopier, recvOptions)
	situation.recvErr = err
	ctx.Logf("zfs recv exit with %T %s", err, err)

	require.NotNil(ctx, err)
	resumeErr, ok := err.(*zfs.RecvFailedWithResumeTokenErr)
	require.True(ctx, ok)
	situation.recvErrDecoded = resumeErr

	return situation
}
