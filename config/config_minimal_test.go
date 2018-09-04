package config

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestConfigEmptyFails(t *testing.T) {
	conf, err := testConfig(t, "\n")
	assert.Nil(t, conf)
	assert.Error(t, err)
}

func TestJobsOnlyWorks(t *testing.T) {
	testValidConfig(t, `
jobs:
- name: push
  type: push
  # snapshot the filesystems matched by the left-hand-side of the mapping
  # every 10m with zrepl_ as prefix
  connect:
    type: tcp
    address: localhost:2342
  filesystems: {
    "pool1/var/db<": true,
    "pool1/usr/home<": true,
    "pool1/usr/home/paranoid": false, #don't backup paranoid user
    "pool1/poudriere/ports<": false #don't backup the ports trees
  }
  snapshotting:
    snapshot_prefix: zrepl_
    interval: 10m
  pruning:
    keep_sender:
    - type: not_replicated
    keep_receiver:
    - type: last_n 
      count: 1
`)
}