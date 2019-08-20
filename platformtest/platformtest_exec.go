package platformtest

import (
	"context"
	"fmt"
	"os/exec"
)

type ex struct {
	log Logger
}

func newEx(log Logger) *ex {
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
	// use circlog and capture stdout
	err := ecmd.Run()
	ee, ok := err.(*exec.ExitError)
	if err != nil && !ok {
		panic(err)
	}
	if expectSuccess && err != nil {
		return fmt.Errorf("expecting no error, got error: %s\n%s", err, ee.Stderr)
	} else if !expectSuccess && err == nil {
		return fmt.Errorf("expecting error, got no error")
	}
	return nil
}
