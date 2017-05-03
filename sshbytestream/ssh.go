package sshbytestream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type Error struct {
	Stderr  []byte
	WaitErr error
}

func (e Error) Error() string {
	return fmt.Sprintf("ssh command failed with error: %v. stderr:\n%s\n", e.WaitErr, e.Stderr)
}

type SSHTransport struct {
	Host         string
	User         string
	Port         uint16
	IdentityFile string
	SSHCommand   string
	Options      []string
}

var SSHCommand string = "ssh"

func Incoming() (wc io.ReadWriteCloser, err error) {
	// derivce ReadWriteCloser from stdin & stdout
	return IncomingReadWriteCloser{}, nil
}

type IncomingReadWriteCloser struct{}

func (f IncomingReadWriteCloser) Read(p []byte) (n int, err error) {
	return os.Stdin.Read(p)
}

func (f IncomingReadWriteCloser) Write(p []byte) (n int, err error) {
	return os.Stdout.Write(p)
}

func (f IncomingReadWriteCloser) Close() (err error) {
	if err = os.Stdin.Close(); err != nil {
		return
	}
	if err = os.Stdout.Close(); err != nil {
		return
	}
	return
}

func Outgoing(remote SSHTransport) (conn io.ReadWriteCloser, err error) {

	ctx, cancel := context.WithCancel(context.Background())

	sshArgs := make([]string, 0, 2*len(remote.Options)+4)
	sshArgs = append(sshArgs,
		"-p", fmt.Sprintf("%d", remote.Port),
		"-q",
		"-i", remote.IdentityFile,
		"-o", "BatchMode=yes",
	)
	for _, option := range remote.Options {
		sshArgs = append(sshArgs, "-o", option)
	}
	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", remote.User, remote.Host))

	var sshCommand = SSHCommand
	if len(remote.SSHCommand) > 0 {
		sshCommand = SSHCommand
	}
	cmd := exec.CommandContext(ctx, sshCommand, sshArgs...)

	// Clear environment of cmd
	cmd.Env = []string{}

	var in io.WriteCloser
	var out io.ReadCloser

	if in, err = cmd.StdinPipe(); err != nil {
		return
	}
	if out, err = cmd.StdoutPipe(); err != nil {
		return
	}

	stderrBuf := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderrBuf

	f := &ForkedSSHReadWriteCloser{
		RemoteStdin:   in,
		RemoteStdout:  out,
		Cancel:        cancel,
		Command:       cmd,
		exitWaitGroup: &sync.WaitGroup{},
	}

	f.exitWaitGroup.Add(1)
	if err = cmd.Start(); err != nil {
		return
	}

	go func() {
		defer f.exitWaitGroup.Done()

		// stderr output is only relevant for errors if the exit code is non-zero
		if err := cmd.Wait(); err != nil {
			f.SSHCommandError = Error{
				Stderr:  stderrBuf.Bytes(),
				WaitErr: err,
			}
		} else {
			f.SSHCommandError = nil
		}
	}()

	return f, nil
}

type ForkedSSHReadWriteCloser struct {
	RemoteStdin     io.Writer
	RemoteStdout    io.Reader
	Command         *exec.Cmd
	Cancel          context.CancelFunc
	exitWaitGroup   *sync.WaitGroup
	SSHCommandError error
}

func (f *ForkedSSHReadWriteCloser) Read(p []byte) (n int, err error) {
	if f.SSHCommandError != nil {
		return 0, f.SSHCommandError
	}
	if n, err = f.RemoteStdout.Read(p); err == io.EOF {
		// the ssh command has exited, but we need to wait for post-portem to finish
		f.exitWaitGroup.Wait()
		if f.SSHCommandError != nil {
			err = f.SSHCommandError
		}
	}
	return
}

func (f *ForkedSSHReadWriteCloser) Write(p []byte) (n int, err error) {
	if f.SSHCommandError != nil {
		return 0, f.SSHCommandError
	}
	if n, err = f.RemoteStdin.Write(p); err == io.EOF {
		// the ssh command has exited, but we need to wait for post-portem to finish
		f.exitWaitGroup.Wait()
		if f.SSHCommandError != nil {
			err = f.SSHCommandError
		}
	}
	return
}

func (f *ForkedSSHReadWriteCloser) Close() (err error) {
	// TODO should check SSHCommandError?
	f.Cancel()
	f.exitWaitGroup.Wait()
	return f.SSHCommandError
}
