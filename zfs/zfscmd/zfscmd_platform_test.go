package zfscmd

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testBin = "./zfscmd_platform_test.bash"

func TestCmdStderrBehaviorOutput(t *testing.T) {

	stdout, err := exec.Command(testBin, "0").Output()
	require.NoError(t, err)
	require.Equal(t, []byte("to stdout\n"), stdout)

	stdout, err = exec.Command(testBin, "1").Output()
	assert.Equal(t, []byte("to stdout\n"), stdout)
	require.Error(t, err)
	ee, ok := err.(*exec.ExitError)
	require.True(t, ok)
	require.Equal(t, ee.Stderr, []byte("to stderr\n"))
}

func TestCmdStderrBehaviorCombinedOutput(t *testing.T) {

	stdio, err := exec.Command(testBin, "0").CombinedOutput()
	require.NoError(t, err)
	require.Equal(t, "to stderr\nto stdout\n", string(stdio))

	stdio, err = exec.Command(testBin, "1").CombinedOutput()
	require.Equal(t, "to stderr\nto stdout\n", string(stdio))
	require.Error(t, err)
	ee, ok := err.(*exec.ExitError)
	require.True(t, ok)
	require.Empty(t, ee.Stderr) // !!!! maybe not what one would expect
}

func TestCmdStderrBehaviorStdoutPipe(t *testing.T) {
	cmd := exec.Command(testBin, "1")
	stdoutPipe, err := cmd.StdoutPipe()
	require.NoError(t, err)
	err = cmd.Start()
	require.NoError(t, err)
	defer cmd.Wait()
	var stdout bytes.Buffer
	_, err = io.Copy(&stdout, stdoutPipe)
	require.NoError(t, err)
	require.Equal(t, "to stdout\n", stdout.String())

	err = cmd.Wait()
	require.Error(t, err)
	ee, ok := err.(*exec.ExitError)
	require.True(t, ok)
	require.Empty(t, ee.Stderr) // !!!!! probably not what one would expect if we only redirect stdout
}

func TestCmdProcessState(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", "echo running; sleep 3600")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	err = cmd.Start()
	require.NoError(t, err)

	r := bufio.NewReader(stdout)
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "running\n", line)

	// we know it's running and sleeping
	cancel()
	err = cmd.Wait()
	t.Logf("wait err %T\n%s", err, err)
	require.Error(t, err)
	ee, ok := err.(*exec.ExitError)
	require.True(t, ok)
	require.NotNil(t, ee.ProcessState)
	require.Contains(t, ee.Error(), "killed")

}
