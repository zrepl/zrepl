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
	os.Exit(0)
	return nil
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
	var stderr io.Reader

	if in, err = cmd.StdinPipe(); err != nil {
		return
	}

	if out, err = cmd.StdoutPipe(); err != nil {
		return
	}
	if stderr, err = cmd.StderrPipe(); err != nil {
		return
	}

	f := ForkedSSHReadWriteCloser{
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
		var b bytes.Buffer
		if _, err := io.Copy(&b, stderr); err != nil {
			panic(err)
		}
		if err := cmd.Wait(); err != nil {
			fmt.Fprintf(os.Stderr, "ssh command exited with error: %v. Stderr:\n%s\n", cmd.ProcessState, b)
			//panic(err) TODO
		}
	}()

	return f, nil
}

type ForkedSSHReadWriteCloser struct {
	RemoteStdin   io.Writer
	RemoteStdout  io.Reader
	Command       *exec.Cmd
	Cancel        context.CancelFunc
	exitWaitGroup *sync.WaitGroup
}

func (f ForkedSSHReadWriteCloser) Read(p []byte) (n int, err error) {
	return f.RemoteStdout.Read(p)
}

func (f ForkedSSHReadWriteCloser) Write(p []byte) (n int, err error) {
	return f.RemoteStdin.Write(p)
}

func (f ForkedSSHReadWriteCloser) Close() (err error) {
	f.Cancel()
	f.exitWaitGroup.Wait()
	return nil
}
