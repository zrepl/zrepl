package zfscmd

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/util/circlog"
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

func TestSigpipe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := CommandContext(ctx, "bash", "-c", "sleep 5; echo invalid input; exit 23")
	r, w, err := os.Pipe()
	require.NoError(t, err)
	output := circlog.MustNewCircularLog(1 << 20)
	cmd.SetStdio(Stdio{
		Stdin:  r,
		Stdout: output,
		Stderr: output,
	})
	err = cmd.Start()
	require.NoError(t, err)
	err = r.Close()
	require.NoError(t, err)

	// the script doesn't read stdin, but this input is almost certainly smaller than the pipe buffer
	const LargerThanPipeBuffer = 1 << 21
	_, err = io.Copy(w, bytes.NewBuffer(bytes.Repeat([]byte("i"), LargerThanPipeBuffer)))
	// => io.Copy is going to block because the pipe buffer is full and the
	//    script is not reading from it
	// => the script is going to exit after 5s
	// => we should expect a broken pipe error from the copier's perspective
	t.Logf("copy err = %T: %s", err, err)
	require.NotNil(t, err)
	require.True(t, strings.Contains(err.Error(), "broken pipe"))

	err = cmd.Wait()
	require.EqualError(t, err, "exit status 23")

}
