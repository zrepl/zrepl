package zfs

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"sync"
)

// A ForkReader is an io.Reader for a forked process's stdout.
// It Wait()s for the process to exit and - if it exits with error - returns this exit error
// on subsequent Read()s.
type ForkReader struct {
	cancelFunc    context.CancelFunc
	cmd           *exec.Cmd
	stdout        io.Reader
	waitErr       error
	exitWaitGroup sync.WaitGroup
}

func NewForkReader(command string, args ...string) (r *ForkReader, err error) {

	r = &ForkReader{}

	var ctx context.Context
	ctx, r.cancelFunc = context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, command, args...)

	stderr := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderr

	if r.stdout, err = cmd.StdoutPipe(); err != nil {
		return
	}

	if err = cmd.Start(); err != nil {
		return
	}
	r.exitWaitGroup.Add(1)

	go func() {
		defer r.exitWaitGroup.Done()
		if err := cmd.Wait(); err != nil {
			os.Stderr.WriteString(err.Error())
			r.waitErr = ZFSError{
				Stderr:  stderr.Bytes(),
				WaitErr: err,
			}
			return
		}
	}()
	return
}

func (r *ForkReader) Read(buf []byte) (n int, err error) {
	if r.waitErr != nil {
		return 0, r.waitErr
	}
	if n, err = r.stdout.Read(buf); err == io.EOF {
		// the command has exited but we need to wait for Wait()ing goroutine to finish
		r.exitWaitGroup.Wait()
		if r.waitErr != nil {
			err = r.waitErr
		}
	}
	return
}
