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

func TestCmd_Pipe(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		cmd          *Cmd
		pipeCmds     [][]string
		pipeLeft     bool
		wantCmdStr   string
		startErr     bool
		waitErr      bool
		assertStdout func(t *testing.T, b []byte)
	}{
		{
			name:       "no error",
			cmd:        CommandContext(ctx, "echo", "foobar"),
			pipeCmds:   [][]string{{"tr", "a-z", "A-Z"}},
			wantCmdStr: "echo foobar | tr a-z A-Z",
			assertStdout: func(t *testing.T, b []byte) {
				assert.Equal(t, "FOOBAR", strings.TrimSpace(string(b)))
			},
		},
		{
			name:       "pipe on the left side",
			cmd:        CommandContext(ctx, "echo", "foobar"),
			pipeCmds:   [][]string{{"true"}},
			pipeLeft:   true,
			wantCmdStr: "true | echo foobar",
		},
		{
			name:       "cmd error",
			cmd:        CommandContext(ctx, "false"),
			pipeCmds:   [][]string{{"tr", "a-z", "A-Z"}},
			wantCmdStr: "false | tr a-z A-Z",
			waitErr:    true,
		},
		{
			name:       "pipe error",
			cmd:        CommandContext(ctx, "echo", "foobar"),
			pipeCmds:   [][]string{{"false"}},
			wantCmdStr: "echo foobar | false",
			waitErr:    true,
		},
		{
			name:       "cmd not found",
			cmd:        CommandContext(ctx, "this-command-doesnt-exists"),
			pipeCmds:   [][]string{{"true"}},
			wantCmdStr: "this-command-doesnt-exists | true",
			startErr:   true,
		},
		{
			name:       "pipe not found",
			cmd:        CommandContext(ctx, "echo", "foobar"),
			pipeCmds:   [][]string{{"this-command-doesnt-exists"}},
			wantCmdStr: "echo foobar | this-command-doesnt-exists",
			startErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			require.NoError(t, err)

			var stderr strings.Builder
			tt.cmd.SetStdio(Stdio{
				Stdout: w,
				Stderr: &stderr,
			})

			if tt.pipeLeft {
				assert.Same(t, tt.cmd, tt.cmd.WithLeftPipe())
			}

			stdout, err := tt.cmd.Pipe(r, &stderr, tt.pipeCmds...)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCmdStr, tt.cmd.String())

			if tt.startErr {
				require.Error(t, tt.cmd.startPipe())
			} else {
				require.NoError(t, tt.cmd.startPipe())
			}
			require.NoError(t, w.Close())

			if tt.startErr {
				return
			}

			b, err := io.ReadAll(stdout)
			require.NoError(t, err)
			require.NoError(t, r.Close())

			if tt.waitErr {
				require.Error(t, tt.cmd.WaitPipe())
			} else {
				require.NoError(t, tt.cmd.WaitPipe())
			}
			assert.Empty(t, stderr.String())

			if tt.assertStdout != nil {
				tt.assertStdout(t, b)
			}
		})
	}
}
