package sshbytestream

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"syscall"

	"github.com/zrepl/zrepl/util"
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
var SSHServerAliveInterval uint = 60

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

type OutgoingSSHByteStream struct {
	c *util.IOCommand
}

func Outgoing(remote SSHTransport) (s OutgoingSSHByteStream, err error) {

	sshArgs := make([]string, 0, 2*len(remote.Options)+4)
	sshArgs = append(sshArgs,
		"-p", fmt.Sprintf("%d", remote.Port),
		"-q",
		"-i", remote.IdentityFile,
		"-o", "BatchMode=yes",
		"-o", fmt.Sprintf("ServerAliveInterval=%d", SSHServerAliveInterval),
	)
	for _, option := range remote.Options {
		sshArgs = append(sshArgs, "-o", option)
	}
	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", remote.User, remote.Host))

	var sshCommand = SSHCommand
	if len(remote.SSHCommand) > 0 {
		sshCommand = remote.SSHCommand
	}

	if s.c, err = util.NewIOCommand(sshCommand, sshArgs, util.IOCommandStderrBufSize); err != nil {
		return
	}

	// Clear environment of cmd, ssh shall not rely on SSH_AUTH_SOCK, etc.
	s.c.Cmd.Env = []string{}

	err = s.c.Start()
	return
}

func (s OutgoingSSHByteStream) Read(p []byte) (n int, err error) {
	return s.c.Read(p)
}

func (s OutgoingSSHByteStream) Write(p []byte) (n int, err error) {
	return s.c.Write(p)
}

func (s OutgoingSSHByteStream) Close() (err error) {
	err = s.c.Close()
	if err == nil || s.c.ExitResult == nil {
		return
	}

	// SSH catches SIGTERM and has different exit codes on different platforms
	ws := s.c.ExitResult.WaitStatus
	switch runtime.GOOS {
	case "linux":
		if ws.ExitStatus() == 128+int(syscall.SIGTERM) { // OpenSSH_7.5p1, OpenSSL 1.1.0f  25 May 2017 Arch Linux
			err = nil
		}
	case "freebsd": // OpenSSH_7.2p2, OpenSSL 1.0.2k-freebsd  26 Jan 2017
		if ws.ExitStatus() == 255 {
			err = nil
		}
	default: // TODO
	}

	return
}
