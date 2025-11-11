package hooks

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LyingCak3/zrepl/internal/config"
	"github.com/LyingCak3/zrepl/internal/daemon/filters"
	"github.com/LyingCak3/zrepl/internal/logger"
	"github.com/LyingCak3/zrepl/internal/util/circlog"
	"github.com/LyingCak3/zrepl/internal/util/envconst"
)

type HookEnvVar string

const (
	EnvType     HookEnvVar = "ZREPL_HOOKTYPE"
	EnvDryRun   HookEnvVar = "ZREPL_DRYRUN"
	EnvFS       HookEnvVar = "ZREPL_FS"
	EnvSnapshot HookEnvVar = "ZREPL_SNAPNAME"
	EnvTimeout  HookEnvVar = "ZREPL_TIMEOUT"
)

type Env map[HookEnvVar]string

func NewHookEnv(edge Edge, phase Phase, dryRun bool, timeout time.Duration, extra Env) Env {
	r := Env{
		EnvTimeout: fmt.Sprintf("%.f", math.Floor(timeout.Seconds())),
	}

	edgeString := edge.StringForPhase(phase)
	r[EnvType] = strings.ToLower(edgeString)

	var dryRunString string
	if dryRun {
		dryRunString = "true"
	} else {
		dryRunString = ""
	}
	r[EnvDryRun] = dryRunString

	for k, v := range extra {
		r[k] = v
	}

	return r
}

type CommandHook struct {
	edge       Edge
	filter     Filter
	errIsFatal bool
	command    string
	timeout    time.Duration
}

type CommandHookReport struct {
	Command                      string
	Args                         []string // currently always empty
	Env                          Env
	Err                          error
	CapturedStdoutStderrCombined []byte
}

func (r *CommandHookReport) String() string {
	// Reproduces a POSIX shell-compatible command line
	var cmdLine strings.Builder
	sep := ""

	// Make sure environment variables are always
	// printed in the same order
	var hookEnvKeys []HookEnvVar
	for k := range r.Env {
		hookEnvKeys = append(hookEnvKeys, k)
	}
	sort.Slice(hookEnvKeys, func(i, j int) bool { return string(hookEnvKeys[i]) < string(hookEnvKeys[j]) })

	for _, k := range hookEnvKeys {
		cmdLine.WriteString(fmt.Sprintf("%s%s='%s'", sep, k, r.Env[k]))
		sep = " "
	}

	cmdLine.WriteString(fmt.Sprintf("%s%s", sep, r.Command))
	for _, a := range r.Args {
		cmdLine.WriteString(fmt.Sprintf("%s'%s'", sep, a))
	}

	var msg string
	if r.Err == nil {
		msg = "command hook"
	} else {
		msg = fmt.Sprintf("command hook failed with %q", r.Err)
	}

	return fmt.Sprintf("%s: \"%s\"", msg, cmdLine.String()) // no %q to make copy-pastable
}
func (r *CommandHookReport) Error() string {
	if r.Err == nil {
		return ""
	}
	return fmt.Sprintf("%s FAILED with error: %s", r.String(), r.Err)
}

func (r *CommandHookReport) HadError() bool {
	return r.Err != nil
}

func NewCommandHook(in *config.HookCommand) (r *CommandHook, err error) {
	r = &CommandHook{
		errIsFatal: in.ErrIsFatal,
		command:    in.Path,
		timeout:    in.Timeout,
	}

	r.filter, err = filters.DatasetMapFilterFromConfig(in.Filesystems)
	if err != nil {
		return nil, fmt.Errorf("cannot parse filesystem filter: %s", err)
	}

	r.edge = Pre | Post

	return r, nil
}

func (h *CommandHook) Filesystems() Filter {
	return h.filter
}

func (h *CommandHook) ErrIsFatal() bool {
	return h.errIsFatal
}

func (h *CommandHook) String() string {
	return h.command
}

func (h *CommandHook) Run(ctx context.Context, edge Edge, phase Phase, dryRun bool, extra Env, state map[interface{}]interface{}) HookReport {
	l := getLogger(ctx).WithField("command", h.command)

	cmdCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	cmdExec := exec.CommandContext(cmdCtx, h.command)

	hookEnv := NewHookEnv(edge, phase, dryRun, h.timeout, extra)
	cmdEnv := os.Environ()
	for k, v := range hookEnv {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}
	cmdExec.Env = cmdEnv

	var scanMutex sync.Mutex
	combinedOutput, err := circlog.NewCircularLog(envconst.Int("ZREPL_MAX_HOOK_LOG_SIZE", MAX_HOOK_LOG_SIZE_DEFAULT))
	if err != nil {
		return &CommandHookReport{Err: err}
	}
	logErrWriter := NewLogWriter(&scanMutex, l, logger.Warn, "stderr")
	logOutWriter := NewLogWriter(&scanMutex, l, logger.Info, "stdout")
	defer logErrWriter.Close()
	defer logOutWriter.Close()

	cmdExec.Stderr = io.MultiWriter(logErrWriter, combinedOutput)
	cmdExec.Stdout = io.MultiWriter(logOutWriter, combinedOutput)

	report := &CommandHookReport{
		Command: h.command,
		Env:     hookEnv,
		// no report.Args
	}

	err = cmdExec.Start()
	if err != nil {
		report.Err = err
		return report
	}

	err = cmdExec.Wait()
	combinedOutputBytes := combinedOutput.Bytes()
	report.CapturedStdoutStderrCombined = make([]byte, len(combinedOutputBytes))
	copy(report.CapturedStdoutStderrCombined, combinedOutputBytes)
	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			report.Err = fmt.Errorf("timed out after %s: %s", h.timeout, err)
			return report
		}
		report.Err = err
		return report
	}

	return report
}
