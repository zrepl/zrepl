package sshbytestream

import (
	"fmt"
	"github.com/zrepl/zrepl/util"
	"io"
	"os"
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

func Outgoing(remote SSHTransport) (c *util.IOCommand, err error) {

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

	if c, err = util.NewIOCommand(sshCommand, sshArgs, util.IOCommandStderrBufSize); err != nil {
		return
	}

	// Clear environment of cmd, ssh shall not rely on SSH_AUTH_SOCK, etc.
	c.Cmd.Env = []string{}

	err = c.Start()
	return
}
