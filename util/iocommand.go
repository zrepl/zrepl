package util

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

// An IOCommand exposes a forked process's std(in|out|err) through the io.ReadWriteCloser interface.
type IOCommand struct {
	Cmd        *exec.Cmd
	CmdContext context.Context
	CmdCancel  context.CancelFunc
	Stdin      io.Writer
	Stdout     io.Reader
	StderrBuf  *bytes.Buffer
}

const IOCommandStderrBufSize = 1024

type IOCommandError struct {
	WaitErr error
	Stderr  []byte
}

func (e IOCommandError) Error() string {
	return fmt.Sprintf("underlying process exited with error: %s\nstderr: %s\n", e.WaitErr, e.Stderr)
}

func RunIOCommand(command string, args ...string) (c *IOCommand, err error) {
	c, err = NewIOCommand(command, args, IOCommandStderrBufSize)
	if err != nil {
		return
	}
	err = c.Start()
	return
}

func NewIOCommand(command string, args []string, stderrBufSize int) (c *IOCommand, err error) {

	if stderrBufSize == 0 {
		stderrBufSize = IOCommandStderrBufSize
	}

	c = &IOCommand{}

	c.CmdContext, c.CmdCancel = context.WithCancel(context.Background())
	c.Cmd = exec.CommandContext(c.CmdContext, command, args...)

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
		if waitErr := c.doWait(); waitErr != nil {
			err = waitErr
		}
	}
	return
}

func (c *IOCommand) doWait() (err error) {
	waitErr := c.Cmd.Wait()
	if waitErr != nil {
		err = IOCommandError{
			WaitErr: waitErr,
			Stderr:  c.StderrBuf.Bytes(),
		}
	}
	return
}

// Write to process's stdin.
// The behavior after Close()ing is undefined
func (c *IOCommand) Write(buf []byte) (n int, err error) {
	return c.Stdin.Write(buf)
}

// Kill the child process and collect its exit status
func (c *IOCommand) Close() error {
	c.CmdCancel()
	return c.doWait()
}
