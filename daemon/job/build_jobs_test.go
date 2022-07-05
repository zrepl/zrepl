package job

import (
	"fmt"
	"path"
	"path/filepath"
	"testing"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/config"
)

func TestValidateReceivingSidesDoNotOverlap(t *testing.T) {
	type testCase struct {
		err   bool
		input []string
	}
	tcs := []testCase{
		{false, nil},
		{false, []string{}},
		{false, []string{""}}, // not our job to determine valid paths
		{false, []string{"a"}},
		{false, []string{"some/path"}},
		{false, []string{"zroot/sink1", "zroot/sink2", "zroot/sink3"}},
		{false, []string{"zroot/foo", "zroot/foobar"}},
		{true, []string{"zroot/b", "zroot/b"}},
		{true, []string{"zroot/foo", "zroot/foo/bar", "zroot/baz"}},
		{false, []string{"a/x", "b/x"}},
		{false, []string{"a", "b"}},
		{true, []string{"a", "a"}},
		{true, []string{"a/x/y", "a/x"}},
		{true, []string{"a/x", "a/x/y"}},
		{true, []string{"a/x", "b/x", "a/x/y"}},
		{true, []string{"a", "a/b", "a/c", "a/b"}},
		{true, []string{"a/b", "a/c", "a/b", "a/d", "a/c"}},
	}

	for _, tc := range tcs {
		t.Logf("input: %v", tc.input)
		err := validateReceivingSidesDoNotOverlap(tc.input)
		if tc.err {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestJobIDErrorHandling(t *testing.T) {
	tmpl := `
jobs:
- name: %s
  type: push
  connect:
    type: local
    listener_name: foo
    client_identity: bar
  filesystems: {"<": true}
  snapshotting:
    type: manual
  pruning:
    keep_sender:
    - type: last_n
      count: 10
    keep_receiver:
    - type: last_n
      count: 10
`
	fill := func(s string) string { return fmt.Sprintf(tmpl, s) }

	type Case struct {
		jobName string
		valid   bool
	}
	cases := []Case{
		{"validjobname", true},
		{"valid with spaces", true},
		{"invalid\twith\ttabs", false},
		{"invalid#withdelimiter", false},
		{"invalid@withdelimiter", false},
		{"withnewline\\nmiddle", false},
		{"withnewline\\n", false},
		{"withslash/", false},
		{"withslash/inthemiddle", false},
		{"/", false},
	}

	for i := range cases {
		t.Run(cases[i].jobName, func(t *testing.T) {
			c := cases[i]

			conf, err := config.ParseConfigBytes([]byte(fill(c.jobName)))
			require.NoError(t, err, "not expecting yaml-config to know about job ids")
			require.NotNil(t, conf)
			jobs, err := JobsFromConfig(conf, config.ParseFlagsNone)

			if c.valid {
				assert.NoError(t, err)
				require.Len(t, jobs, 1)
				assert.Equal(t, c.jobName, jobs[0].Name())
			} else {
				t.Logf("error: %s", err)
				assert.Error(t, err)
				assert.Nil(t, jobs)
			}

		})
	}

}

func TestSampleConfigsAreBuiltWithoutErrors(t *testing.T) {
	paths, err := filepath.Glob("../../config/samples/*")
	if err != nil {
		t.Errorf("glob failed: %+v", err)
	}

	type additionalCheck struct {
		state int
		test  func(t *testing.T, jobs []Job)
	}
	additionalChecks := map[string]*additionalCheck{
		"bandwidth_limit.yml": {test: testSampleConfig_BandwidthLimit},
	}

	for _, p := range paths {

		if path.Ext(p) != ".yml" {
			t.Logf("skipping file %s", p)
			continue
		}

		filename := path.Base(p)
		t.Logf("checking for presence additonal checks for file %q", filename)
		additionalCheck := additionalChecks[filename]
		if additionalCheck == nil {
			t.Logf("no additional checks")
		} else {
			t.Logf("additional check present")
			additionalCheck.state = 1
		}

		t.Run(p, func(t *testing.T) {
			c, err := config.ParseConfig(p)
			if err != nil {
				t.Fatalf("error parsing %s:\n%+v", p, err)
			}

			t.Logf("file: %s", p)
			t.Log(pretty.Sprint(c))

			jobs, err := JobsFromConfig(c, config.ParseFlagsNoCertCheck)
			t.Logf("jobs: %#v", jobs)
			require.NoError(t, err)

			if additionalCheck != nil {
				additionalCheck.test(t, jobs)
				additionalCheck.state = 2
			}
		})

	}

	for basename, c := range additionalChecks {
		if c.state == 0 {
			panic("univisited additional check " + basename)
		}
	}

}

func testSampleConfig_BandwidthLimit(t *testing.T, jobs []Job) {
	require.Len(t, jobs, 3)

	{
		limitedSink, ok := jobs[0].(*PassiveSide)
		require.True(t, ok, "%T", jobs[0])
		limitedSinkMode, ok := limitedSink.mode.(*modeSink)
		require.True(t, ok, "%T", limitedSink)

		assert.Equal(t, int64(12345), limitedSinkMode.receiverConfig.BandwidthLimit.Max)
		assert.Equal(t, int64(1<<17), limitedSinkMode.receiverConfig.BandwidthLimit.BucketCapacity)
	}

	{
		limitedPush, ok := jobs[1].(*ActiveSide)
		require.True(t, ok, "%T", jobs[1])
		limitedPushMode, ok := limitedPush.mode.(*modePush)
		require.True(t, ok, "%T", limitedPush)

		assert.Equal(t, int64(54321), limitedPushMode.senderConfig.BandwidthLimit.Max)
		assert.Equal(t, int64(1024), limitedPushMode.senderConfig.BandwidthLimit.BucketCapacity)
	}

	{
		unlimitedSink, ok := jobs[2].(*PassiveSide)
		require.True(t, ok, "%T", jobs[2])
		unlimitedSinkMode, ok := unlimitedSink.mode.(*modeSink)
		require.True(t, ok, "%T", unlimitedSink)

		max := unlimitedSinkMode.receiverConfig.BandwidthLimit.Max
		assert.Less(t, max, int64(0), max, "unlimited mode <=> negative value for .Max, see bandwidthlimit.Config")
	}

}

func TestReplicationOptions(t *testing.T) {
	tmpl := `
jobs:
- name: foo
  type: push
  connect:
    type: local
    listener_name: foo
    client_identity: bar
  filesystems: {"<": true}
  %s
  snapshotting:
    type: manual
  pruning:
    keep_sender:
    - type: last_n
      count: 10
    keep_receiver:
    - type: last_n
      count: 10
`

	type Test struct {
		name        string
		input       string
		expectOk    func(t *testing.T, a *ActiveSide, m *modePush)
		expectError bool
	}

	tests := []Test{
		{
			name: "defaults",
			input: `
  replication: {}
`,
			expectOk: func(t *testing.T, a *ActiveSide, m *modePush) {},
		},
		{
			name: "steps_zero",
			input: `
  replication:
    concurrency:
      steps: 0
`,
			expectError: true,
		},
		{
			name: "size_estimates_zero",
			input: `
  replication:
    concurrency:
      size_estimates: 0
`,
			expectError: true,
		},
		{
			name: "custom_values",
			input: `
  replication:
    concurrency:
      steps: 23
      size_estimates: 42
`,
			expectOk: func(t *testing.T, a *ActiveSide, m *modePush) {
				assert.Equal(t, 23, a.replicationDriverConfig.StepQueueConcurrency)
				assert.Equal(t, 42, m.plannerPolicy.SizeEstimationConcurrency)
			},
		},
		{
			name: "negative_values_forbidden",
			input: `
  replication:
    concurrency:
      steps: -23
      size_estimates: -42
`,
			expectError: true,
		},
	}

	fill := func(s string) string { return fmt.Sprintf(tmpl, s) }

	for _, ts := range tests {
		t.Run(ts.name, func(t *testing.T) {
			assert.True(t, (ts.expectError) != (ts.expectOk != nil))

			cstr := fill(ts.input)
			t.Logf("testing config:\n%s", cstr)
			c, err := config.ParseConfigBytes([]byte(cstr))
			require.NoError(t, err)
			jobs, err := JobsFromConfig(c, config.ParseFlagsNone)
			if ts.expectOk != nil {
				require.NoError(t, err)
				require.NotNil(t, c)
				require.NoError(t, err)
				require.Len(t, jobs, 1)
				a := jobs[0].(*ActiveSide)
				m := a.mode.(*modePush)
				ts.expectOk(t, a, m)
			} else if ts.expectError {
				require.Error(t, err)
			} else {
				t.Fatalf("test must define expectOk or expectError")
			}

		})
	}

}
