package config

import (
	"testing"
)

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
    type: manual
  pruning:
    keep_sender:
    - type: not_replicated
    keep_receiver:
    - type: last_n
      count: 1
`)
}
