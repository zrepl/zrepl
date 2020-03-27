// Package zfscmd provides a wrapper around packate os/exec.
// Functionality provided by the wrapper:
// - logging start and end of command execution
// - status report of active commands
// - prometheus metrics of runtimes
package zfscmd

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/zrepl/zrepl/util/circlog"
)

type Cmd struct {
	cmd                       *exec.Cmd
	ctx                       context.Context
	mtx                       sync.RWMutex
	startedAt, waitReturnedAt time.Time
}

func CommandContext(ctx context.Context, name string, arg ...string) *Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	return &Cmd{cmd: cmd, ctx: ctx}
}

// err.(*exec.ExitError).Stderr will NOT be set
func (c *Cmd) CombinedOutput() (o []byte, err error) {
	o, err = c.cmd.CombinedOutput()
	return
}

// err.(*exec.ExitError).Stderr will be set
func (c *Cmd) Output() (o []byte, err error) {
	o, err = c.cmd.Output()
	return
}

// Careful: err.(*exec.ExitError).Stderr will not be set, even if you don't open an StderrPipe
func (c *Cmd) StdoutPipeWithErrorBuf() (p io.ReadCloser, errBuf *circlog.CircularLog, err error) {
	p, err = c.cmd.StdoutPipe()
	errBuf = circlog.MustNewCircularLog(1 << 15)
	c.cmd.Stderr = errBuf
	return p, errBuf, err
}

type Stdio struct {
	Stdin  io.ReadCloser
	Stdout io.Writer
	Stderr io.Writer
}

func (c *Cmd) SetStdio(stdio Stdio) {
	c.cmd.Stdin = stdio.Stdin
	c.cmd.Stderr = stdio.Stderr
	c.cmd.Stdout = stdio.Stdout
}

func (c *Cmd) String() string {
	return strings.Join(c.cmd.Args, " ") // includes argv[0] if initialized with CommandContext, that's the only way we o it
}

func (c *Cmd) log() Logger {
	return getLogger(c.ctx).WithField("cmd", c.String())
}

func (c *Cmd) Start() (err error) {
	startPreLogging(c, time.Now())

	err = c.cmd.Start()
	now := time.Now()

	c.mtx.Lock()
	c.startedAt = now
	c.mtx.Unlock()

	startPostReport(c, err, now)
	startPostLogging(c, err, now)
	return err
}

// only call this after a successful call to .Start()
func (c *Cmd) Process() *os.Process {
	if c.startedAt.IsZero() {
		panic("calling Process() only allowed after successful call to Start()")
	}
	return c.cmd.Process
}

func (c *Cmd) Wait() (err error) {
	waitPreLogging(c, time.Now())

	err = c.cmd.Wait()
	now := time.Now()

	if !c.waitReturnedAt.IsZero() {
		// ignore duplicate waits
		return
	}

	c.mtx.Lock()
	c.waitReturnedAt = now
	c.mtx.Unlock()

	waitPostReport(c, now)
	waitPostLogging(c, err, now)
	waitPostPrometheus(c, err, now)
	return err
}

// returns 0 if the command did not yet finish
func (c *Cmd) Runtime() time.Duration {
	if c.waitReturnedAt.IsZero() {
		return 0
	}
	return c.waitReturnedAt.Sub(c.startedAt)
}
