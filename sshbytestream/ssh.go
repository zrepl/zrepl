package sshbytestream

import (
	"io"
	"os"
	"os/exec"
	"github.com/zrepl/zrepl/model"
	"context"
	"fmt"
	"bytes"
	"sync"
)

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

func (f IncomingReadWriteCloser)  Close() (err error) {
	os.Exit(0)
	return nil
}

func Outgoing(name string, remote model.SSHTransport) (conn io.ReadWriteCloser, err error) {

	ctx, cancel := context.WithCancel(context.Background())

	sshArgs := make([]string, 0, 2 * len(remote.Options) + len(remote.TransportOpenCommand) + 4)
	sshArgs = append(sshArgs,
		"-p", fmt.Sprintf("%d", remote.Port),
		"-o", "BatchMode=yes",
	)
	for _,option := range remote.Options {
		sshArgs = append(sshArgs, "-o", option)
	}
	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", remote.User, remote.Host))
	sshArgs = append(sshArgs, remote.TransportOpenCommand...)

	cmd := exec.CommandContext(ctx, SSHCommand, sshArgs...)

	var in io.WriteCloser
	var out io.ReadCloser

	if in, err = cmd.StdinPipe(); err != nil {
		return
	}

	if out,err = cmd.StdoutPipe(); err != nil {
		return
	}


	f := ForkedSSHReadWriteCloser{
		RemoteStdin: in,
		RemoteStdout: out,
		Cancel: cancel,
		Command: cmd,
		exitWaitGroup: &sync.WaitGroup{},
	}

	f.exitWaitGroup.Add(1)

	go func() {
		defer f.exitWaitGroup.Done()
		stderr, _ := cmd.StderrPipe()
		var b bytes.Buffer
		if err := cmd.Run(); err != nil {
			io.Copy(&b, stderr)
			fmt.Println(b.String())
			fmt.Printf("%v\n", cmd.ProcessState)
			//panic(err)
		}
	}()

	return f, nil
}

type  ForkedSSHReadWriteCloser struct {
	RemoteStdin io.Writer
	RemoteStdout io.Reader
	Command *exec.Cmd
	Cancel 	context.CancelFunc
	exitWaitGroup *sync.WaitGroup
}

func (f ForkedSSHReadWriteCloser) Read(p []byte) (n int, err error) {
	return f.RemoteStdout.Read(p)
}

func (f ForkedSSHReadWriteCloser) Write(p []byte) (n int, err error) {
	return f.RemoteStdin.Write(p)
}

func (f ForkedSSHReadWriteCloser)  Close() (err error) {
	f.Cancel()
	f.exitWaitGroup.Wait()
	return nil
}