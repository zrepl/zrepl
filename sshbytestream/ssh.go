package sshbytestream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
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

func Outgoing(remote SSHTransport) (f *ForkExecReadWriter, err error) {

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
	ctx, cancel := context.WithCancel(context.Background())
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

	f = &ForkExecReadWriter{
		Stdin:         in,
		Stdout:        out,
		Command:       cmd,
		CommandCancel: cancel,
		StderrBuf:     stderrBuf,
	}

	err = cmd.Start()
	return
}

type ForkExecReadWriter struct {
	Command       *exec.Cmd
	CommandCancel context.CancelFunc
	Stdin         io.Writer
	Stdout        io.Reader
	StderrBuf     *bytes.Buffer
}

func (f *ForkExecReadWriter) Read(buf []byte) (n int, err error) {
	n, err = f.Stdout.Read(buf)
	if err == io.EOF {
		waitErr := f.Command.Wait()
		if waitErr != nil {
			err = Error{
				WaitErr: waitErr,
				Stderr:  f.StderrBuf.Bytes(),
			}
		}
	}
	return
}

func (f *ForkExecReadWriter) Write(p []byte) (n int, err error) {
	return f.Stdin.Write(p)
}

func (f *ForkExecReadWriter) Close() error {
	f.CommandCancel()
	return nil
}
