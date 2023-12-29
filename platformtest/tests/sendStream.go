package tests

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"syscall"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/util/nodefault"
	"github.com/zrepl/zrepl/zfs"
)

func sendStreamTest(ctx *platformtest.Context) *zfs.SendStream {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
		DESTROYROOT
		CREATEROOT
		+  "sender"
	`)

	fs := fmt.Sprintf("%s/sender", ctx.RootDataset)

	fsmpo, err := zfs.ZFSGetMountpoint(ctx, fs)
	require.NoError(ctx, err)

	writeDummyData(path.Join(fsmpo.Mountpoint, "dummy.data"), 1<<26)
	mustSnapshot(ctx, fs+"@1")
	snap := fsversion(ctx, fs, "@1")
	snapSendArg := snap.ToSendArgVersion()

	sendArgs, err := zfs.ZFSSendArgsUnvalidated{
		FS:   fs,
		From: nil,
		To:   &snapSendArg,
		ZFSSendFlags: zfs.ZFSSendFlags{
			Encrypted: &nodefault.Bool{B: false},
		},
	}.Validate(ctx)
	require.NoError(ctx, err)

	sendStream, err := zfs.ZFSSend(ctx, sendArgs)
	require.NoError(ctx, err)

	return sendStream

}

func SendStreamCloseAfterBlockedOnPipeWrite(ctx *platformtest.Context) {

	sendStream := sendStreamTest(ctx)

	// let the pipe buffer fill and the zfs process block uninterruptibly

	ctx.Logf("waiting for pipe write to block")
	time.Sleep(5 * time.Second) // XXX need a platform-neutral way to detect that the pipe is full and the writer is blocked

	ctx.Logf("closing send stream")
	err := sendStream.Close() // this is what this test case is about
	ctx.Logf("close error: %T %s", err, err)
	require.NoError(ctx, err)

	exitErrZfsError := sendStream.TestOnly_ExitErr()
	require.Contains(ctx, exitErrZfsError.Error(), "signal")
	require.Error(ctx, exitErrZfsError)
	exitErr, ok := exitErrZfsError.WaitErr.(*exec.ExitError)
	require.True(ctx, ok)
	if exitErr.Exited() {
		// some ZFS impls (FreeBSD 12) behaves that way
		return
	}

	// ProcessState is only available after exit
	// => use as proxy that the process was wait()ed upon and is gone
	ctx.Logf("%#v", exitErr.ProcessState)
	require.NotNil(ctx, exitErr.ProcessState)
	// and let's verify that the process got killed, so that we know it was the call to .Close() above
	waitStatus := exitErr.ProcessState.Sys().(syscall.WaitStatus)
	ctx.Logf("wait status: %#v", waitStatus)
	ctx.Logf("exit status: %v", waitStatus.ExitStatus())
	require.True(ctx, waitStatus.Signaled())
	switch waitStatus.Signal() {
	case unix.SIGKILL:
		fallthrough
	case unix.SIGPIPE:
		// ok

	default:
		ctx.Errorf("%T %s\n%v", waitStatus.Signal(), waitStatus.Signal(), waitStatus.Signal())
		ctx.FailNow()
	}
}

func SendStreamCloseAfterEOFRead(ctx *platformtest.Context) {
	sendStream := sendStreamTest(ctx)

	_, err := io.Copy(io.Discard, sendStream)
	require.NoError(ctx, err)

	var buf [128]byte
	n, err := sendStream.Read(buf[:])
	require.Zero(ctx, n)
	require.Equal(ctx, io.EOF, err)

	err = sendStream.Close()
	require.NoError(ctx, err)

	n, err = sendStream.Read(buf[:])
	require.Zero(ctx, n)
	require.Equal(ctx, os.ErrClosed, err, "same read error should be returned")
}

func SendStreamMultipleCloseAfterEOF(ctx *platformtest.Context) {
	sendStream := sendStreamTest(ctx)

	_, err := io.Copy(io.Discard, sendStream)
	require.NoError(ctx, err)

	var buf [128]byte
	n, err := sendStream.Read(buf[:])
	require.Zero(ctx, n)
	require.Equal(ctx, io.EOF, err)

	err = sendStream.Close()
	require.NoError(ctx, err)

	err = sendStream.Close()
	require.Equal(ctx, os.ErrClosed, err)
}

func SendStreamMultipleCloseBeforeEOF(ctx *platformtest.Context) {

	sendStream := sendStreamTest(ctx)

	err := sendStream.Close()
	require.NoError(ctx, err)

	err = sendStream.Close()
	require.Equal(ctx, os.ErrClosed, err)
}

type failingReadCloser struct {
	err error
}

var _ io.ReadCloser = &failingReadCloser{}

func (c *failingReadCloser) Read(p []byte) (int, error) { return 0, c.err }
func (c *failingReadCloser) Close() error               { return c.err }

func SendStreamNonEOFReadErrorHandling(ctx *platformtest.Context) {

	sendStream := sendStreamTest(ctx)

	var buf [128]byte
	n, err := sendStream.Read(buf[:])
	require.Equal(ctx, len(buf), n)
	require.NoError(ctx, err)

	var mockError = fmt.Errorf("taeghaefow4piesahwahjocu7ul5tiachaiLipheijae8ooZ8Pies8shohGee9feeTeirai5aiFeiyaecai4kiaLoh4azeih0tea")
	mock := &failingReadCloser{err: mockError}
	orig := sendStream.TestOnly_ReplaceStdoutReader(mock)

	n, err = sendStream.Read(buf[:])
	require.Equal(ctx, 0, n)
	require.Equal(ctx, mockError, err)

	if sendStream.TestOnly_ReplaceStdoutReader(orig) != mock {
		panic("incorrect test impl")
	}

	err = sendStream.Close()
	require.NoError(ctx, err) // if we can't kill the child then this will be a flaky test, but let's assume we can kill the child

	err = sendStream.Close()
	require.Equal(ctx, os.ErrClosed, err)
}
