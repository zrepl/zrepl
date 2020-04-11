package hooks_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/hooks"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/zfs"
)

type comparisonAssertionFunc func(require.TestingT, interface{}, interface{}, ...interface{})
type valueAssertionFunc func(require.TestingT, interface{}, ...interface{})

type expectStep struct {
	ExpectedEdge hooks.Edge
	ExpectStatus hooks.StepStatus
	OutputTest   valueAssertionFunc
	ErrorTest    valueAssertionFunc
}

type testCase struct {
	Name           string
	Config         []string
	IsSlow         bool
	SuppressOutput bool

	ExpectCallbackSkipped bool
	ExpectHadFatalErr     bool
	ExpectHadError        bool
	ExpectStepReports     []expectStep
}

func curryLeft(f comparisonAssertionFunc, expected interface{}) valueAssertionFunc {
	return curry(f, expected, false)
}

func curryRight(f comparisonAssertionFunc, expected interface{}) valueAssertionFunc {
	return curry(f, expected, true)
}

func curry(f comparisonAssertionFunc, expected interface{}, right bool) (ret valueAssertionFunc) {
	ret = func(t require.TestingT, s interface{}, v ...interface{}) {
		var x interface{}
		var y interface{}
		if right {
			x = s
			y = expected
		} else {
			x = expected
			y = s
		}

		if len(v) > 0 {
			f(t, x, y, v)
		} else {
			f(t, x, y)
		}
	}
	return
}

func TestHooks(t *testing.T) {
	ctx, end := logging.WithTaskFromStack(context.Background())
	defer end()

	testFSName := "testpool/testdataset"
	testSnapshotName := "testsnap"

	tmpl, err := template.New("TestHooks").Parse(`
jobs:
- name: TestHooks
  type: snap
  filesystems: {"<": true}
  snapshotting:
    type: periodic
    interval: 1m
    prefix: zrepl_snapjob_
    hooks:
    {{- template "List" . }}
  pruning:
    keep:
      - type: last_n
        count: 10
`)
	if err != nil {
		panic(err)
	}

	regexpTest := func(s string) valueAssertionFunc {
		return curryLeft(require.Regexp, regexp.MustCompile(s))
	}

	containsTest := func(s string) valueAssertionFunc {
		return curryRight(require.Contains, s)
	}

	testTable := []testCase{
		testCase{
			Name: "no_hooks",
			ExpectStepReports: []expectStep{
				{ExpectedEdge: hooks.Callback, ExpectStatus: hooks.StepOk},
			},
		},
		testCase{
			Name:           "timeout",
			IsSlow:         true,
			ExpectHadError: true,
			Config:         []string{`{type: command, path: {{.WorkDir}}/test/test-timeout.sh, timeout: 2s}`},
			ExpectStepReports: []expectStep{
				expectStep{
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepErr,
					OutputTest:   containsTest(fmt.Sprintf("TEST pre_testing %s@%s ZREPL_TIMEOUT=2", testFSName, testSnapshotName)),
					ErrorTest:    regexpTest(`timed out after 2(.\d+)?s`),
				},
				expectStep{ExpectedEdge: hooks.Callback, ExpectStatus: hooks.StepOk},
				expectStep{
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepSkippedDueToPreErr,
				},
			},
		},
		testCase{
			Name:           "check_env",
			Config:         []string{`{type: command, path: {{.WorkDir}}/test/test-report-env.sh}`},
			ExpectHadError: false,
			ExpectStepReports: []expectStep{
				expectStep{
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepOk,
					OutputTest:   containsTest(fmt.Sprintf("TEST pre_testing %s@%s", testFSName, testSnapshotName)),
				},
				expectStep{ExpectedEdge: hooks.Callback, ExpectStatus: hooks.StepOk},
				expectStep{
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepOk,
					OutputTest:   containsTest(fmt.Sprintf("TEST post_testing %s@%s", testFSName, testSnapshotName)),
				},
			},
		},

		testCase{
			Name:                  "nonfatal_pre_error_continues",
			ExpectCallbackSkipped: false,
			ExpectHadError:        true,
			Config:                []string{`{type: command, path: {{.WorkDir}}/test/test-error.sh}`},
			ExpectStepReports: []expectStep{
				expectStep{
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepErr,
					OutputTest:   containsTest(fmt.Sprintf("TEST ERROR pre_testing %s@%s", testFSName, testSnapshotName)),
					ErrorTest:    regexpTest("^command hook invocation.*exit status 1$"),
				},
				expectStep{ExpectedEdge: hooks.Callback, ExpectStatus: hooks.StepOk},
				expectStep{
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepSkippedDueToPreErr, // post-edge is not executed for failing pre-edge
				},
			},
		},

		testCase{
			Name:                  "pre_error_fatal_skips_subsequent_pre_edges_and_callback_and_its_post_edge_and_post_edges",
			ExpectCallbackSkipped: true,
			ExpectHadFatalErr:     true,
			ExpectHadError:        true,
			Config: []string{
				`{type: command, path: {{.WorkDir}}/test/test-error.sh, err_is_fatal: true}`,
				`{type: command, path: {{.WorkDir}}/test/test-report-env.sh}`,
			},
			ExpectStepReports: []expectStep{
				expectStep{
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepErr,
					OutputTest:   containsTest(fmt.Sprintf("TEST ERROR pre_testing %s@%s", testFSName, testSnapshotName)),
					ErrorTest:    regexpTest("^command hook invocation.*exit status 1$"),
				},
				expectStep{
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepSkippedDueToFatalErr,
				},
				expectStep{ExpectedEdge: hooks.Callback, ExpectStatus: hooks.StepSkippedDueToFatalErr},
				expectStep{
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepSkippedDueToFatalErr,
				},
				expectStep{
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepSkippedDueToFatalErr,
				},
			},
		},

		testCase{
			Name:              "post_error_fails_are_ignored_even_if_fatal",
			ExpectHadFatalErr: false, // only occurs during Post, so it's not a fatal error
			ExpectHadError:    true,
			Config: []string{
				`{type: command, path: {{.WorkDir}}/test/test-post-error.sh, err_is_fatal: true}`,
				`{type: command, path: {{.WorkDir}}/test/test-report-env.sh}`,
			},
			ExpectStepReports: []expectStep{
				expectStep{
					// No-action run of test-post-error.sh
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepOk,
					OutputTest:   require.Empty,
				},
				expectStep{
					// Pre run of test-report-env.sh
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepOk,
					OutputTest:   containsTest(fmt.Sprintf("TEST pre_testing %s@%s", testFSName, testSnapshotName)),
				},
				expectStep{ExpectedEdge: hooks.Callback, ExpectStatus: hooks.StepOk},
				expectStep{
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepOk,
					OutputTest:   containsTest(fmt.Sprintf("TEST post_testing %s@%s", testFSName, testSnapshotName)),
				},
				expectStep{
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepErr,
					OutputTest:   containsTest(fmt.Sprintf("TEST ERROR post_testing %s@%s", testFSName, testSnapshotName)),
					ErrorTest:    regexpTest("^command hook invocation.*exit status 1$"),
				},
			},
		},

		testCase{
			Name:   "cleanup_check_env",
			Config: []string{`{type: command, path: {{.WorkDir}}/test/test-report-env.sh}`},
			ExpectStepReports: []expectStep{
				expectStep{
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepOk,
					OutputTest:   containsTest(fmt.Sprintf("TEST pre_testing %s@%s", testFSName, testSnapshotName)),
				},
				expectStep{ExpectedEdge: hooks.Callback, ExpectStatus: hooks.StepOk},
				expectStep{
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepOk,
					OutputTest:   containsTest(fmt.Sprintf("TEST post_testing %s@%s", testFSName, testSnapshotName)),
				},
			},
		},

		testCase{
			Name:              "pre_error_cancels_post",
			Config:            []string{`{type: command, path: {{.WorkDir}}/test/test-pre-error-post-ok.sh}`},
			ExpectHadError:    true,
			ExpectHadFatalErr: false,
			ExpectStepReports: []expectStep{
				expectStep{
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepErr,
					OutputTest:   containsTest(fmt.Sprintf("TEST ERROR pre_testing %s@%s", testFSName, testSnapshotName)),
					ErrorTest:    regexpTest("^command hook invocation.*exit status 1$"),
				},
				expectStep{ExpectedEdge: hooks.Callback, ExpectStatus: hooks.StepOk},
				expectStep{
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepSkippedDueToPreErr,
				},
			},
		},

		testCase{
			Name: "pre_error_does_not_cancel_other_posts_but_itself",
			Config: []string{
				`{type: command, path: {{.WorkDir}}/test/test-report-env.sh}`,
				`{type: command, path: {{.WorkDir}}/test/test-pre-error-post-ok.sh}`,
			},
			ExpectHadError:    true,
			ExpectHadFatalErr: false,
			ExpectStepReports: []expectStep{
				expectStep{
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepOk,
					OutputTest:   containsTest(fmt.Sprintf("TEST pre_testing %s@%s", testFSName, testSnapshotName)),
				},
				expectStep{
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepErr,
					OutputTest:   containsTest(fmt.Sprintf("TEST ERROR pre_testing %s@%s", testFSName, testSnapshotName)),
					ErrorTest:    regexpTest("^command hook invocation.*exit status 1$"),
				},
				expectStep{ExpectedEdge: hooks.Callback, ExpectStatus: hooks.StepOk},
				expectStep{
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepSkippedDueToPreErr,
				},
				expectStep{
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepOk,
					OutputTest:   containsTest(fmt.Sprintf("TEST post_testing %s@%s", testFSName, testSnapshotName)),
				},
			},
		},

		testCase{
			Name:              "exceed_buffer_limit",
			SuppressOutput:    true,
			Config:            []string{`{type: command, path: {{.WorkDir}}/test/test-large-stdout.sh}`},
			ExpectHadError:    false,
			ExpectHadFatalErr: false,
			ExpectStepReports: []expectStep{
				expectStep{
					ExpectedEdge: hooks.Pre,
					ExpectStatus: hooks.StepOk,
					OutputTest: func(t require.TestingT, s interface{}, v ...interface{}) {
						require.Len(t, s, 1<<20)
					},
				},
				expectStep{ExpectedEdge: hooks.Callback, ExpectStatus: hooks.StepOk},
				expectStep{
					// No-action run of above hook
					ExpectedEdge: hooks.Post,
					ExpectStatus: hooks.StepOk,
					OutputTest:   require.Empty,
				},
			},
		},

		/*
			Following not intended to test functionality of
			filter package. Only to demonstrate that hook
			filters are being applied. The following should
			result in NO hooks running. If it does run, a
			fatal hooks.RunReport will be returned.
		*/
		testCase{
			Name: "exclude_all_filesystems",
			Config: []string{
				`{type: command, path: {{.WorkDir}}/test/test-error.sh, err_is_fatal: true, filesystems: {"<": false}}`,
			},
			ExpectStepReports: []expectStep{
				expectStep{
					ExpectedEdge: hooks.Callback,
					ExpectStatus: hooks.StepOk,
				},
			},
		},
	}

	parseHookConfig := func(t *testing.T, in string) *config.Config {
		t.Helper()
		conf, err := config.ParseConfigBytes([]byte(in))
		require.NoError(t, err)
		require.NotNil(t, conf)
		return conf
	}

	fillHooks := func(tt *testCase) string {
		// make hook path absolute
		cwd, err := os.Getwd()
		if err != nil {
			panic("os.Getwd() failed")
		}
		var hooksTmpl string = "\n"
		for _, l := range tt.Config {
			hooksTmpl += fmt.Sprintf("    - %s\n", l)
		}
		tmpl.New("List").Parse(hooksTmpl)

		var outBytes bytes.Buffer
		data := struct {
			WorkDir string
		}{
			WorkDir: cwd,
		}
		if err := tmpl.Execute(&outBytes, data); err != nil {
			panic(err)
		}

		return outBytes.String()
	}

	var c *config.Config
	fs, err := zfs.NewDatasetPath(testFSName)
	require.NoError(t, err)

	log := logger.NewTestLogger(t)

	var cbReached bool
	cb := hooks.NewCallbackHookForFilesystem("testcallback", fs, func(_ context.Context) error {
		cbReached = true
		return nil
	})

	hookEnvExtra := hooks.Env{
		hooks.EnvFS:       fs.ToString(),
		hooks.EnvSnapshot: testSnapshotName,
	}

	for _, tt := range testTable {
		if testing.Short() && tt.IsSlow {
			continue
		}

		t.Run(tt.Name, func(t *testing.T) {
			c = parseHookConfig(t, fillHooks(&tt))
			snp := c.Jobs[0].Ret.(*config.SnapJob).Snapshotting.Ret.(*config.SnapshottingPeriodic)
			hookList, err := hooks.ListFromConfig(&snp.Hooks)
			require.NoError(t, err)

			filteredHooks, err := hookList.CopyFilteredForFilesystem(fs)
			require.NoError(t, err)
			plan, err := hooks.NewPlan(&filteredHooks, hooks.PhaseTesting, cb, hookEnvExtra)
			require.NoError(t, err)
			t.Logf("REPORT PRE EXECUTION:\n%s", plan.Report())

			cbReached = false

			if testing.Verbose() && !tt.SuppressOutput {
				ctx = logging.WithLoggers(ctx, logging.SubsystemLoggersWithUniversalLogger(log))
			}
			plan.Run(ctx, false)
			report := plan.Report()

			t.Logf("REPORT POST EXECUTION:\n%s", report)

			/*
			 * TEST ASSERTIONS
			 */

			t.Logf("len(runReports)=%v", len(report))
			t.Logf("len(tt.ExpectStepReports)=%v", len(tt.ExpectStepReports))
			require.Equal(t, len(tt.ExpectStepReports), len(report), "ExpectStepReports must be same length as expected number of hook runs, excluding possible Callback")

			// Check if callback ran, when required
			if tt.ExpectCallbackSkipped {
				require.False(t, cbReached, "callback ran but should not have run")
			} else {
				require.True(t, cbReached, "callback should have run but did not")
			}

			// Check if a fatal run error occurred and was expected
			require.Equal(t, tt.ExpectHadFatalErr, report.HadFatalError(), "non-matching HadFatalError")
			require.Equal(t, tt.ExpectHadError, report.HadError(), "non-matching HadError")

			if tt.ExpectHadFatalErr {
				require.True(t, tt.ExpectHadError, "ExpectHadFatalErr implies ExpectHadError")
			}
			if !tt.ExpectHadError {
				require.False(t, tt.ExpectHadFatalErr, "!ExpectHadError implies !ExpectHadFatalErr")
			}

			// Iterate through each expected hook run
			for i, hook := range tt.ExpectStepReports {
				t.Logf("expecting report conforming to %v", hook)

				exp, act := hook.ExpectStatus, report[i].Status
				require.Equal(t, exp, act, "%s != %s", exp, act)

				// Check for required ExpectedEdge
				require.NotZero(t, hook.ExpectedEdge, "each hook must have an ExpectedEdge")
				require.Equal(t, hook.ExpectedEdge, report[i].Edge,
					"incorrect edge: expected %q, actual %q", hook.ExpectedEdge.String(), report[i].Edge.String(),
				)

				// Check for expected output
				if hook.OutputTest != nil {
					require.IsType(t, (*hooks.CommandHookReport)(nil), report[i].Report)
					chr := report[i].Report.(*hooks.CommandHookReport)
					hook.OutputTest(t, string(chr.CapturedStdoutStderrCombined))
				}

				// Check for expected errors
				if hook.ErrorTest != nil {
					hook.ErrorTest(t, string(report[i].Report.Error()))
				}
			}

		})
	}
}
