package config

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestSnapshotting(t *testing.T) {
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
  pruning:
    keep_sender:
    - type: last_n
      count: 10
    keep_receiver:
    - type: last_n
      count: 10
`
	manual := `
  snapshotting:
    type: manual
`
	periodic := `
  snapshotting:
    type: periodic
    prefix: zrepl_
    interval: 10m
`

	fillSnapshotting := func(s string) string {return fmt.Sprintf(tmpl, s)}
	var c *Config

	t.Run("manual", func(t *testing.T) {
		c = testValidConfig(t, fillSnapshotting(manual))
		snm := c.Jobs[0].Ret.(*PushJob).Snapshotting.Ret.(*SnapshottingManual)
		assert.Equal(t, "manual", snm.Type)
	})

	t.Run("periodic", func(t *testing.T) {
		c = testValidConfig(t, fillSnapshotting(periodic))
		snp := c.Jobs[0].Ret.(*PushJob).Snapshotting.Ret.(*SnapshottingPeriodic)
		assert.Equal(t, "periodic", snp.Type)
		assert.Equal(t, 10*time.Minute, snp.Interval)
		assert.Equal(t, "zrepl_" , snp.Prefix)
	})

}
