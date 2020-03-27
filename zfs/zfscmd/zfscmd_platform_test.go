package zfscmd

import (
	"bytes"
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
