package tests

import (
	"io"
	"math/rand"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/util/limitio"
	"github.com/zrepl/zrepl/zfs"
)

func sendArgVersion(ctx *platformtest.Context, fs, relName string) zfs.ZFSSendArgVersion {
	guid, err := zfs.ZFSGetGUID(ctx, fs, relName)
	if err != nil {
		panic(err)
	}
	return zfs.ZFSSendArgVersion{
		RelName: relName,
		GUID:    guid,
	}
}

func fsversion(ctx *platformtest.Context, fs, relname string) zfs.FilesystemVersion {
	v, err := zfs.ZFSGetFilesystemVersion(ctx, fs+relname)
	if err != nil {
		panic(err)
	}
	return v
}

func mustDatasetPath(fs string) *zfs.DatasetPath {
	p, err := zfs.NewDatasetPath(fs)
	if err != nil {
		panic(err)
	}
	return p
}

func mustSnapshot(ctx *platformtest.Context, snap string) {
	if err := zfs.EntityNamecheck(snap, zfs.EntityTypeSnapshot); err != nil {
		panic(err)
	}
	comps := strings.Split(snap, "@")
	if len(comps) != 2 {
		panic(comps)
	}
	err := zfs.ZFSSnapshot(ctx, mustDatasetPath(comps[0]), comps[1], false)
	if err != nil {
		panic(err)
	}
}

func mustGetFilesystemVersion(ctx *platformtest.Context, snapOrBookmark string) zfs.FilesystemVersion {
	v, err := zfs.ZFSGetFilesystemVersion(ctx, snapOrBookmark)
	check(err)
	return v
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
	sendArgs         zfs.ZFSSendArgsUnvalidated
	recvOpts         zfs.RecvOptions
	sendErr, recvErr error
	recvErrDecoded   *zfs.RecvFailedWithResumeTokenErr
}

func makeDummyDataSnapshots(ctx *platformtest.Context, sendFS string) (situation dummySnapshotSituation) {

	situation.sendFS = sendFS
	sendFSMount, err := zfs.ZFSGetMountpoint(ctx, sendFS)
	require.NoError(ctx, err)
	require.True(ctx, sendFSMount.Mounted)

	const dummyLen = int64(10 * (1 << 20))
	situation.dummyDataLen = dummyLen

	writeDummyData(path.Join(sendFSMount.Mountpoint, "dummy_data"), dummyLen)
	mustSnapshot(ctx, sendFS+"@a snapshot")
	snapA := sendArgVersion(ctx, sendFS, "@a snapshot")
	situation.snapA = &snapA

	writeDummyData(path.Join(sendFSMount.Mountpoint, "dummy_data"), dummyLen)
	mustSnapshot(ctx, sendFS+"@b snapshot")
	snapB := sendArgVersion(ctx, sendFS, "@b snapshot")
	situation.snapB = &snapB

	return situation
}

func makeResumeSituation(ctx *platformtest.Context, src dummySnapshotSituation, recvFS string, sendArgs zfs.ZFSSendArgsUnvalidated, recvOptions zfs.RecvOptions) *resumeSituation {

	situation := &resumeSituation{}

	situation.sendArgs = sendArgs
	situation.recvOpts = recvOptions
	require.True(ctx, recvOptions.SavePartialRecvState, "this method would be pointless otherwise")
	require.Equal(ctx, sendArgs.FS, src.sendFS)
	sendArgsValidated, err := sendArgs.Validate(ctx)
	situation.sendErr = err
	if err != nil {
		return situation
	}

	copier, err := zfs.ZFSSend(ctx, sendArgsValidated)
	situation.sendErr = err
	if err != nil {
		return situation
	}

	limitedCopier := limitio.ReadCloser(copier, src.dummyDataLen/2)
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

func versionRelnamesSorted(versions []zfs.FilesystemVersion) []string {
	var vstrs []string
	for _, v := range versions {
		vstrs = append(vstrs, v.RelName())
	}
	sort.Strings(vstrs)
	return vstrs
}

func datasetToStringSortedTrimPrefix(prefix *zfs.DatasetPath, paths []*zfs.DatasetPath) []string {
	var pstrs []string
	for _, p := range paths {
		trimmed := p.Copy()
		trimmed.TrimPrefix(prefix)
		if trimmed.Length() == 0 {
			continue
		}
		pstrs = append(pstrs, trimmed.ToString())
	}
	sort.Strings(pstrs)
	return pstrs
}

func mustAddToSFilter(ctx *platformtest.Context, f *filters.DatasetMapFilter, fs string) {
	err := f.Add(fs, "ok")
	require.NoError(ctx, err)
}
