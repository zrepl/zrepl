package config

import (
	"testing"
	"github.com/stretchr/testify/assert"
	"time"
)

func TestRPC (t *testing.T) {
	conf := testValidConfig(t, `
jobs:
- name: pull_servers
  type: pull
  replication:
    connect:
      type: tcp
      address: "server1.foo.bar:8888"
      rpc:
        timeout: 20s # different form default, should merge
    root_dataset: "pool2/backup_servers"
    interval: 10m
  pruning:
    keep_sender:
      - type: not_replicated
    keep_receiver:
      - type: last_n
        count: 100

- name: pull_servers2
  type: pull
  replication:
    connect:
      type: tcp
      address: "server1.foo.bar:8888"
      rpc:
        tx_chunk_size: 0xabcd # different from default, should merge
    root_dataset: "pool2/backup_servers"
    interval: 10m
  pruning:
    keep_sender:
    - type: not_replicated
    keep_receiver:
    - type: last_n
      count: 100
`)

	assert.Equal(t, 20*time.Second, conf.Jobs[0].Ret.(*PullJob).Replication.Connect.Ret.(*TCPConnect).RPC.Timeout)
	assert.Equal(t, uint(0xabcd), conf.Jobs[1].Ret.(*PullJob).Replication.Connect.Ret.(*TCPConnect).RPC.TxChunkSize)
	defConf := RPCConfig{}
	Default(&defConf)
	assert.Equal(t, defConf.Timeout, conf.Global.RPC.Timeout)
}

func TestGlobal_DefaultRPCConfig(t *testing.T) {
	assert.NotPanics(t, func() {
		var c RPCConfig
		Default(&c)
		assert.NotNil(t, c)
		assert.Equal(t, c.TxChunkSize, uint(1)<<15)
	})
}
