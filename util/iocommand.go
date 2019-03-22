package util

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/zrepl/zrepl/util/envconst"
)

// An IOCommand exposes a forked process's std(in|out|err) through the io.ReadWriteCloser interface.
type IOCommand struct {
	Cmd        *exec.Cmd
	kill       context.CancelFunc
	Stdin      io.WriteCloser
	Stdout     io.ReadCloser
	StderrBuf  *bytes.Buffer
	ExitResult *IOCommandExitResult
}

const IOCommandStderrBufSize = 1024

type IOCommandError struct {
	WaitErr error
	Stderr  []byte
}

type IOCommandExitResult struct {
	Error      error
	WaitStatus syscall.WaitStatus
}

func (e IOCommandError) Error() string {
	return fmt.Sprintf("underlying process exited with error: %s\nstderr: %s\n", e.WaitErr, e.Stderr)
}

func RunIOCommand(ctx context.Context, command string, args ...string) (c *IOCommand, err error) {
	c, err = NewIOCommand(ctx, command, args, IOCommandStderrBufSize)
	if err != nil {
		return
	}
	err = c.Start()
	return
}

func NewIOCommand(ctx context.Context, command string, args []string, stderrBufSize int) (c *IOCommand, err error) {

	if stderrBufSize == 0 {
		stderrBufSize = IOCommandStderrBufSize
	}

	c = &IOCommand{}

	ctx, c.kill = context.WithCancel(ctx)
	c.Cmd = exec.CommandContext(ctx, command, args...)

	if c.Stdout, err = c.Cmd.StdoutPipe(); err != nil {
		return
	}

	if c.Stdin, err = c.Cmd.StdinPipe(); err != nil {
		return
	}

	c.StderrBuf = bytes.NewBuffer(make([]byte, 0, stderrBufSize))
	c.Cmd.Stderr = c.StderrBuf

	return

}

func (c *IOCommand) Start() (err error) {
	if err = c.Cmd.Start(); err != nil {
		return
	}
	return
}

// Read from process's stdout.
// The behavior after Close()ing is undefined
func (c *IOCommand) Read(buf []byte) (n int, err error) {
	n, err = c.Stdout.Read(buf)
	if err == io.EOF {
		if waitErr := c.doWait(context.Background()); waitErr != nil {
			err = waitErr
		}
	}
	return
}

func (c *IOCommand) doWait(ctx context.Context) (err error) {
	go func() {
		dl, ok := ctx.Deadline()
		if !ok {
			return
		}
		time.Sleep(dl.Sub(time.Now()))
		c.kill()
		c.Stdout.Close()
		c.Stdin.Close()
	}()
	waitErr := c.Cmd.Wait()
	var wasUs bool = false
	var waitStatus syscall.WaitStatus
	if c.Cmd.ProcessState == nil {
		fmt.Fprintf(os.Stderr, "util.IOCommand: c.Cmd.ProcessState is nil after c.Cmd.Wait()\n")
	}
	if c.Cmd.ProcessState != nil {
		sysSpecific := c.Cmd.ProcessState.Sys()
		var ok bool
		waitStatus, ok = sysSpecific.(syscall.WaitStatus)
		if !ok {
			fmt.Fprintf(os.Stderr, "util.IOCommand: c.Cmd.ProcessState.Sys() could not be converted to syscall.WaitStatus: %T\n", sysSpecific)
			os.Stderr.Sync()
			panic(sysSpecific) // this can only be true if we are not on UNIX, and we don't support that
		}
		wasUs = waitStatus.Signaled() && waitStatus.Signal() == syscall.SIGTERM // in Close()
	}

	if waitErr != nil && !wasUs {
		err = IOCommandError{
			WaitErr: waitErr,
			Stderr:  c.StderrBuf.Bytes(),
		}
	}

	c.ExitResult = &IOCommandExitResult{
		Error:      err, // is still empty if waitErr was due to signalling
		WaitStatus: waitStatus,
	}
	return
}

// Write to process's stdin.
// The behavior after Close()ing is undefined
func (c *IOCommand) Write(buf []byte) (n int, err error) {
	return c.Stdin.Write(buf)
}

// Terminate the child process and collect its exit status
// It is safe to call Close() multiple times.
func (c *IOCommand) Close() (err error) {
	if c.Cmd.ProcessState == nil {
		// racy...
		err = syscall.Kill(c.Cmd.Process.Pid, syscall.SIGTERM)
		ctx, cancel := context.WithTimeout(context.Background(), envconst.Duration("IOCOMMAND_TIMEOUT", 10*time.Second))
		defer cancel()
		return c.doWait(ctx)
	} else {
		return c.ExitResult.Error
	}
}
