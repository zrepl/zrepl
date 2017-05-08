package zfs

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
)

// A ForkReader is an io.Reader for a forked process's stdout.
// It Wait()s for the process to exit and - if it exits with error - returns this exit error
// on subsequent Read()s.
type ForkExecReader struct {
	Cmd       *exec.Cmd
	InStream  io.Reader
	StderrBuf *bytes.Buffer
}

func NewForkExecReader(command string, args ...string) (r *ForkExecReader, err error) {

	r = &ForkExecReader{}

	r.Cmd = exec.Command(command, args...)

	r.InStream, err = r.Cmd.StdoutPipe()
	if err != nil {
		return
	}

	r.StderrBuf = bytes.NewBuffer(make([]byte, 0, 1024))
	r.Cmd.Stderr = r.StderrBuf

	if err = r.Cmd.Start(); err != nil {
		return
	}

	return

}

type ForkExecReaderError struct {
	WaitErr error
	Stderr  []byte
}

func (e ForkExecReaderError) Error() string {
	return fmt.Sprintf("underlying process exited with error: %s\nstderr: %s\n", e.WaitErr, e.Stderr)
}

func (t *ForkExecReader) Read(buf []byte) (n int, err error) {
	n, err = t.InStream.Read(buf)
	if err == io.EOF {
		waitErr := t.Cmd.Wait()
		if waitErr != nil {
			err = ForkExecReaderError{
				WaitErr: waitErr,
				Stderr:  t.StderrBuf.Bytes(),
			}
		}
	}
	return
}
