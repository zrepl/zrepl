package sshdirect

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/util/circlog"
)

type Endpoint struct {
	Host         string
	User         string
	Port         uint16
	IdentityFile string
	SSHCommand   string
	Options      []string
	RunCommand   []string
}

func (e Endpoint) CmdArgs() (cmd string, args []string, env []string) {

	if e.SSHCommand != "" {
		cmd = e.SSHCommand
	} else {
		cmd = "ssh"
	}

	args = make([]string, 0, 2*len(e.Options)+4)
	args = append(args,
		"-p", fmt.Sprintf("%d", e.Port),
		"-T",
		"-i", e.IdentityFile,
		"-o", "BatchMode=yes",
	)
	for _, option := range e.Options {
		args = append(args, "-o", option)
	}
	args = append(args, fmt.Sprintf("%s@%s", e.User, e.Host))

	args = append(args, e.RunCommand...)

	env = []string{}

	return
}

type SSHConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	shutdownMtx    sync.Mutex
	shutdownResult *shutdownResult // TODO not used anywhere
	cmdCancel      context.CancelFunc
}

const go_network string = "netssh"

type clientAddr struct {
	pid int
}

func (a clientAddr) Network() string {
	return go_network
}

func (a clientAddr) String() string {
	return fmt.Sprintf("pid=%d", a.pid)
}

func (conn *SSHConn) LocalAddr() net.Addr {
	proc := conn.cmd.Process
	if proc == nil {
		return clientAddr{-1}
	}
	return clientAddr{proc.Pid}
}

func (conn *SSHConn) RemoteAddr() net.Addr {
	return conn.LocalAddr()
}

// Read implements io.Reader.
// It returns *IOError for any non-nil error that is != io.EOF.
func (conn *SSHConn) Read(p []byte) (int, error) {
	n, err := conn.stdout.Read(p)
	if err != nil && err != io.EOF {
		return n, &IOError{err}
	}
	return n, err
}

// Write implements io.Writer.
// It returns *IOError for any error != nil.
func (conn *SSHConn) Write(p []byte) (int, error) {
	n, err := conn.stdin.Write(p)
	if err != nil {
		return n, &IOError{err}
	}
	return n, err
}

func (conn *SSHConn) CloseWrite() error {
	return conn.stdin.Close()
}

type deadliner interface {
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
}

func (conn *SSHConn) SetReadDeadline(t time.Time) error {
	// type assertion is covered by test TestExecCmdPipesDeadlineBehavior
	return conn.stdout.(deadliner).SetReadDeadline(t)
}

func (conn *SSHConn) SetWriteDeadline(t time.Time) error {
	// type assertion is covered by test TestExecCmdPipesDeadlineBehavior
	return conn.stdin.(deadliner).SetWriteDeadline(t)
}

func (conn *SSHConn) SetDeadline(t time.Time) error {
	// try both
	rerr := conn.SetReadDeadline(t)
	werr := conn.SetWriteDeadline(t)
	if rerr != nil {
		return rerr
	}
	if werr != nil {
		return werr
	}
	return nil
}

func (conn *SSHConn) Close() error {
	conn.shutdownProcess()
	return nil // FIXME: waitError will be non-zero because we signaled it, shutdownProcess needs to distinguish that
}

type shutdownResult struct {
	waitErr error
}

func (conn *SSHConn) shutdownProcess() *shutdownResult {
	conn.shutdownMtx.Lock()
	defer conn.shutdownMtx.Unlock()

	if conn.shutdownResult != nil {
		return conn.shutdownResult
	}

	wait := make(chan error, 1)
	go func() {
		if err := conn.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			// TODO log error
			return
		}
		wait <- conn.cmd.Wait()
	}()

	timeout := time.NewTimer(1 * time.Second) // FIXME const
	defer timeout.Stop()

	select {
	case waitErr := <-wait:
		conn.shutdownResult = &shutdownResult{waitErr}
	case <-timeout.C:
		conn.cmdCancel()
		waitErr := <-wait // reuse existing Wait invocation, must not call twice
		conn.shutdownResult = &shutdownResult{waitErr}
	}
	return conn.shutdownResult
}

// Cmd returns the underlying *exec.Cmd (the ssh client process)
// Use read-only, should not be necessary for regular users.
func (conn *SSHConn) Cmd() *exec.Cmd {
	return conn.cmd
}

// CmdCancel bypasses the normal shutdown mechanism of SSHConn
// (that is, calling Close) and cancels the process's context,
// which usually results in SIGKILL being sent to the process.
// Intended for integration tests, regular users shouldn't use it.
func (conn *SSHConn) CmdCancel() {
	conn.cmdCancel()
}

const bannerMessageLen = 31

var messages = make(map[string][]byte)

func mustMessage(str string) []byte {
	if len(str) > bannerMessageLen {
		panic("message length must be smaller than bannerMessageLen")
	}
	if _, ok := messages[str]; ok {
		panic("duplicate message")
	}
	var buf bytes.Buffer
	n, _ := buf.WriteString(str)
	if n != len(str) {
		panic("message must only contain ascii / 8-bit chars")
	}
	buf.Write(bytes.Repeat([]byte{0}, bannerMessageLen-n))
	return buf.Bytes()
}

var banner_msg = mustMessage("SSDIRECTHCON_HELO")
var proxy_error_msg = mustMessage("SSDIRECTHCON_PROXY_ERROR") /* FIXME irrelevant, was copy-pasta */
var begin_msg = mustMessage("SSDIRECTHCON_BEGIN")

type SSHError struct {
	RWCError      error
	WhileActivity string
}

// Error() will try to present a one-line error message unless ssh stderr output is longer than one line
func (e *SSHError) Error() string {

	exitErr, ok := e.RWCError.(*exec.ExitError)
	if !ok {
		return fmt.Sprintf("ssh: %s", e.RWCError)
	}

	ws := exitErr.ProcessState.Sys().(syscall.WaitStatus)
	var wsmsg string
	if ws.Exited() {
		wsmsg = fmt.Sprintf("(exit status %d)", ws.ExitStatus())
	} else {
		wsmsg = fmt.Sprintf("(%s)", ws.Signal())
	}

	haveSSHMessage := len(exitErr.Stderr) > 0
	sshOnelineStderr := false
	if i := bytes.Index(exitErr.Stderr, []byte("\n")); i == len(exitErr.Stderr)-1 {
		sshOnelineStderr = true
	}
	stderr := bytes.TrimSpace(exitErr.Stderr)

	if haveSSHMessage {
		if sshOnelineStderr {
			return fmt.Sprintf("ssh: '%s' %s", stderr, wsmsg) // FIXME proper single-quoting
		} else {
			return fmt.Sprintf("ssh %s\n%s", wsmsg, stderr)
		}
	}

	return fmt.Sprintf("ssh terminated without stderr output %s", wsmsg)

}

type ProtocolError struct {
	What string
}

func (e ProtocolError) Error() string {
	return e.What
}

// Dial connects to the remote endpoint where it expects a command executing Proxy().
// Dial performs a handshake consisting of the exchange of banner messages before returning the connection.
// If the handshake cannot be completed before dialCtx is Done(), the underlying ssh command is killed
// and the dialCtx.Err() returned.
// If the handshake completes, dialCtx's deadline does not affect the returned connection.
//
// Errors returned are either dialCtx.Err(), or intances of ProtocolError or *SSHError
func Dial(dialCtx context.Context, endpoint Endpoint) (*SSHConn, error) {

	sshCmd, sshArgs, sshEnv := endpoint.CmdArgs()
	commandCtx, commandCancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(commandCtx, sshCmd, sshArgs...)
	cmd.Env = sshEnv
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderrBuf, err := circlog.NewCircularLog(1 << 15)
	if err != nil {
		panic(err) // wrong API usage
	}
	cmd.Stderr = stderrBuf

	if err = cmd.Start(); err != nil {
		return nil, err
	}
	cmdWaitErrOrIOErr := func(ioErr error, what string) *SSHError {
		werr := cmd.Wait()
		if werr, ok := werr.(*exec.ExitError); ok {
			werr.Stderr = []byte(stderrBuf.String())
			return &SSHError{werr, what}
		}
		return &SSHError{ioErr, what}
	}

	confErrChan := make(chan error, 1)
	go func() {
		defer close(confErrChan)
		var buf bytes.Buffer
		if _, err := io.CopyN(&buf, stdout, int64(len(banner_msg))); err != nil {
			confErrChan <- cmdWaitErrOrIOErr(err, "read banner")
			return
		}
		resp := buf.Bytes()
		switch {
		case bytes.Equal(resp, banner_msg):
			break
		case bytes.Equal(resp, proxy_error_msg):
			_ = cmdWaitErrOrIOErr(nil, "")
			confErrChan <- ProtocolError{"proxy error, check remote configuration"}
			return
		default:
			_ = cmdWaitErrOrIOErr(nil, "")
			confErrChan <- ProtocolError{fmt.Sprintf("unknown banner message: %v", resp)}
			return
		}
		buf.Reset()
		buf.Write(begin_msg)
		if _, err := io.Copy(stdin, &buf); err != nil {
			confErrChan <- cmdWaitErrOrIOErr(err, "send begin message")
			return
		}
	}()

	select {
	case <-dialCtx.Done():

		commandCancel()
		// cancelling will make one of the calls in above goroutine fail,
		// and the goroutine will send the error to confErrChan
		//
		// ignore the error and return the cancellation cause

		// draining always terminates because we know the channel is always closed
		for _ = range confErrChan {
		}

		// TODO collect stderr in this case
		// can probably extend *SSHError for this but need to implement net.Error

		return nil, dialCtx.Err()

	case err := <-confErrChan:
		if err != nil {
			commandCancel()
			return nil, err
		}
	}

	return &SSHConn{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		cmdCancel: commandCancel,
	}, nil
}

type Connecter struct {
	s        *yamux.Session
	endpoint Endpoint
}

var _ transport.Connecter = (*Connecter)(nil)

func NewConnecter(ctx context.Context, endpoint Endpoint) (*Connecter, error) {
	conn, err := Dial(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	s, err := yamux.Client(conn, nil)
	if err != nil {
		return nil, err
	}
	return &Connecter{
		s:        s,
		endpoint: endpoint,
	}, nil
}

type fakeWire struct {
	net.Conn
}

func (w *fakeWire) CloseWrite() error {
	time.Sleep(1*time.Second) // HACKY
	return fmt.Errorf("fakeWire does not support CloseWrite")
}

func (c *Connecter) Connect(ctx context.Context) (transport.Wire, error) {
	conn, err := c.s.Open()
	return &fakeWire{conn}, err
}
