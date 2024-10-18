package platformtest

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/zrepl/zrepl/internal/util/circlog"
)

type ex struct {
	log Logger
}

func NewEx(log Logger) Execer {
	return &ex{log}
}

func (e *ex) RunExpectSuccessNoOutput(ctx context.Context, cmd string, args ...string) error {
	return e.runNoOutput(true, ctx, cmd, args...)
}

func (e *ex) RunExpectFailureNoOutput(ctx context.Context, cmd string, args ...string) error {
	return e.runNoOutput(false, ctx, cmd, args...)
}

func (e *ex) runNoOutput(expectSuccess bool, ctx context.Context, cmd string, args ...string) error {
	log := e.log.WithField("command", fmt.Sprintf("%q %q", cmd, args))
	log.Debug("begin executing")
	defer log.Debug("done executing")
	ecmd := exec.CommandContext(ctx, cmd, args...)
	buf, _ := circlog.NewCircularLog(32 << 10)
	ecmd.Stdout, ecmd.Stderr = buf, buf
	err := ecmd.Run()
	log.WithField("output", buf.String()).Debug("command output")
	if _, ok := err.(*exec.ExitError); err != nil && !ok {
		panic(err)
	}
	if expectSuccess && err != nil {
		return fmt.Errorf("expecting no error, got error: %s\n%s", err, buf.String())
	} else if !expectSuccess && err == nil {
		return fmt.Errorf("expecting error, got no error")
	}
	return nil
}
