package job

import (
	"fmt"
	"testing"

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
			jobs, err := JobsFromConfig(conf)

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
