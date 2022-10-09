package config

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

	periodicDaily := `
  snapshotting:
    type: periodic
    prefix: zrepl_
    interval: 1d
`

	hooks := `
  snapshotting:
    type: periodic
    prefix: zrepl_
    interval: 10m
    hooks:
    - type: command
      path: /tmp/path/to/command
    - type: command
      path: /tmp/path/to/command
      filesystems: { "zroot<": true, "<": false }
    - type: postgres-checkpoint
      dsn: "host=localhost port=5432 user=postgres sslmode=disable"
      filesystems: {
          "tank/postgres/data11": true
      }
    - type: mysql-lock-tables
      dsn: "root@tcp(localhost)/"
      filesystems: {
        "tank/mysql": true
      }
`

	fillSnapshotting := func(s string) string { return fmt.Sprintf(tmpl, s) }
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
		assert.Equal(t, 10*time.Minute, snp.Interval.Duration())
		assert.Equal(t, "zrepl_", snp.Prefix)
	})

	t.Run("periodicDaily", func(t *testing.T) {
		c = testValidConfig(t, fillSnapshotting(periodicDaily))
		snp := c.Jobs[0].Ret.(*PushJob).Snapshotting.Ret.(*SnapshottingPeriodic)
		assert.Equal(t, "periodic", snp.Type)
		assert.Equal(t, 24*time.Hour, snp.Interval.Duration())
		assert.Equal(t, "zrepl_", snp.Prefix)
	})

	t.Run("hooks", func(t *testing.T) {
		c = testValidConfig(t, fillSnapshotting(hooks))
		hs := c.Jobs[0].Ret.(*PushJob).Snapshotting.Ret.(*SnapshottingPeriodic).Hooks
		assert.Equal(t, hs[0].Ret.(*HookCommand).Filesystems["<"], true)
		assert.Equal(t, hs[1].Ret.(*HookCommand).Filesystems["zroot<"], true)
		assert.Equal(t, hs[2].Ret.(*HookPostgresCheckpoint).Filesystems["tank/postgres/data11"], true)
		assert.Equal(t, hs[3].Ret.(*HookMySQLLockTables).Filesystems["tank/mysql"], true)
	})

}
