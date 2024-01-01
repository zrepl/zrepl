// Package zfscmd provides a wrapper around packate os/exec.
// Functionality provided by the wrapper:
// - logging start and end of command execution
// - status report of active commands
// - prometheus metrics of runtimes
package zfscmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/zrepl/zrepl/daemon/logging/trace"
	"github.com/zrepl/zrepl/util/circlog"
)

type Cmd struct {
	cmd                                      *exec.Cmd
	ctx                                      context.Context
	mtx                                      sync.RWMutex
	startedAt, waitStartedAt, waitReturnedAt time.Time
	waitReturnEndSpanCb                      trace.DoneFunc

	pipeCmds []*exec.Cmd
	pipeLeft bool
}

func CommandContext(ctx context.Context, name string, arg ...string) *Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	return &Cmd{cmd: cmd, ctx: ctx}
}

// err.(*exec.ExitError).Stderr will NOT be set
func (c *Cmd) CombinedOutput() (o []byte, err error) {
	c.startPre(false)
	c.startPost(nil)
	c.waitPre()
	o, err = c.cmd.CombinedOutput()
	c.waitPost(err)
	return
}

// err.(*exec.ExitError).Stderr will be set
func (c *Cmd) Output() (o []byte, err error) {
	c.startPre(false)
	c.startPost(nil)
	c.waitPre()
	o, err = c.cmd.Output()
	c.waitPost(err)
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
	if len(c.pipeCmds) == 0 {
		return strings.Join(c.cmd.Args, " ") // includes argv[0] if initialized with CommandContext, that's the only way we o it
	}

	var s strings.Builder
	if c.pipeLeft {
		for _, cmd := range c.pipeCmds {
			s.WriteString(strings.Join(cmd.Args, " "))
			s.WriteString(" | ")
		}
		s.WriteString(strings.Join(c.cmd.Args, " "))
	} else {
		s.WriteString(strings.Join(c.cmd.Args, " "))
		for _, cmd := range c.pipeCmds {
			s.WriteString(" | ")
			s.WriteString(strings.Join(cmd.Args, " "))
		}
	}

	return s.String()
}

func (c *Cmd) log() Logger {
	return getLogger(c.ctx).WithField("cmd", c.String())
}

// Start the command.
//
// This creates a new trace.WithTask as a child task of the ctx passed to CommandContext.
// If the process is successfully started (err == nil), it is the CALLER'S RESPONSIBILITY to ensure that
// the spawned process does not outlive the ctx's trace.Task.
//
// If this method returns an error, the Cmd instance is invalid. Start must not be called repeatedly.
func (c *Cmd) Start() (err error) {
	c.startPre(true)
	err = c.startPipe()
	c.startPost(err)
	return err
}

// Get the underlying os.Process.
//
// Only call this method after a successful call to .Start().
func (c *Cmd) Process() *os.Process {
	if c.startedAt.IsZero() {
		panic("calling Process() only allowed after successful call to Start()")
	}
	return c.cmd.Process
}

// Blocking wait for the process to exit.
// May be called concurrently and repeatly (exec.Cmd.Wait() semantics apply).
//
// Only call this method after a successful call to .Start().
func (c *Cmd) Wait() (err error) {
	c.waitPre()
	err = c.WaitPipe()
	c.waitPost(err)
	return err
}

func (c *Cmd) startPre(newTask bool) {
	if newTask {
		// avoid explosion of tasks with name c.String()
		c.ctx, c.waitReturnEndSpanCb = trace.WithTaskAndSpan(c.ctx, "zfscmd", c.String())
	} else {
		c.ctx, c.waitReturnEndSpanCb = trace.WithSpan(c.ctx, c.String())
	}
	startPreLogging(c, time.Now())
}

func (c *Cmd) startPost(err error) {

	now := time.Now()
	c.startedAt = now

	startPostReport(c, err, now)
	startPostLogging(c, err, now)

	if err != nil {
		c.waitReturnEndSpanCb()
	}
}

func (c *Cmd) waitPre() {
	now := time.Now()

	// ignore duplicate waits
	c.mtx.Lock()
	// ignore duplicate waits
	if !c.waitStartedAt.IsZero() {
		c.mtx.Unlock()
		return
	}
	c.waitStartedAt = now
	c.mtx.Unlock()

	waitPreLogging(c, now)
}

type usage struct {
	total_secs, system_secs, user_secs float64
}

func (c *Cmd) waitPost(err error) {
	now := time.Now()

	c.mtx.Lock()
	// ignore duplicate waits
	if !c.waitReturnedAt.IsZero() {
		c.mtx.Unlock()
		return
	}
	c.waitReturnedAt = now
	c.mtx.Unlock()

	// build usage
	var u usage
	{
		var s *os.ProcessState
		if err == nil {
			s = c.cmd.ProcessState
		} else if ee, ok := err.(*exec.ExitError); ok {
			s = ee.ProcessState
		}

		if s == nil {
			u = usage{
				total_secs:  c.Runtime().Seconds(),
				system_secs: -1,
				user_secs:   -1,
			}
		} else {
			u = usage{
				total_secs:  c.Runtime().Seconds(),
				system_secs: s.SystemTime().Seconds(),
				user_secs:   s.UserTime().Seconds(),
			}
		}
	}

	waitPostReport(c, u, now)
	waitPostLogging(c, u, err, now)
	waitPostPrometheus(c, u, err, now)

	// must be last because c.ctx might be used by other waitPost calls
	c.waitReturnEndSpanCb()
}

// returns 0 if the command did not yet finish
func (c *Cmd) Runtime() time.Duration {
	if c.waitReturnedAt.IsZero() {
		return 0
	}
	return c.waitReturnedAt.Sub(c.startedAt)
}

func (c *Cmd) TestOnly_ExecCmd() *exec.Cmd {
	return c.cmd
}

func (c *Cmd) Pipe(
	stdin io.ReadCloser, stderr io.Writer, cmds ...[]string,
) (io.ReadCloser, error) {
	for _, pipeCmd := range c.buildPipeCmds(cmds) {
		r, err := pipeCmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf(
				"failed create stdout pipe for %q: %w", pipeCmd.String(), err)
		}
		pipeCmd.Stdin = stdin
		pipeCmd.Stderr = stderr
		c.pipeCmds = append(c.pipeCmds, pipeCmd)
		stdin = r
	}
	return stdin, nil
}

func (c *Cmd) buildPipeCmds(cmds [][]string) []*exec.Cmd {
	pipeCmds := make([]*exec.Cmd, len(cmds))
	for i := range cmds {
		name := cmds[i][0]
		var args []string
		if len(cmds[i]) > 1 {
			args = cmds[i][1:]
		}
		pipeCmds[i] = exec.CommandContext(c.ctx, name, args...)
	}
	return pipeCmds
}

func (c *Cmd) startPipe() error {
	if err := c.cmd.Start(); err != nil {
		return err
	}
	for _, cmd := range c.pipeCmds {
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed start %q: %w", cmd.String(), err)
		}
	}
	return nil
}

func (c *Cmd) WaitPipe() error {
	var firstErr error
	if c.cmd.Process != nil {
		if err := c.cmd.Wait(); err != nil {
			firstErr = err
		}
	}
	for _, cmd := range c.pipeCmds {
		if cmd.Process == nil {
			break
		}
		if err := cmd.Wait(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed wait %q: %w", cmd.String(), err)
		}
	}
	return firstErr
}

func (c *Cmd) WithLeftPipe() *Cmd {
	c.pipeLeft = true
	return c
}
