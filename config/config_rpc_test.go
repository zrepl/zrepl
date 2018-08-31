package config

import (
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestRPC(t *testing.T) {
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

- type: sink
  name: "laptop_sink"
  replication:
    root_dataset: "pool2/backup_laptops"
    serve:
      type: tcp
      listen: "192.168.122.189:8888"
      clients: {
		"10.23.42.23":"client1"
      }
      rpc:
        rx_structured_max: 0x2342

`)

	assert.Equal(t, 20*time.Second, conf.Jobs[0].Ret.(*PullJob).Replication.Connect.Ret.(*TCPConnect).RPC.Timeout)
	assert.Equal(t, uint32(0xabcd), conf.Jobs[1].Ret.(*PullJob).Replication.Connect.Ret.(*TCPConnect).RPC.TxChunkSize)
	assert.Equal(t, uint32(0x2342), conf.Jobs[2].Ret.(*SinkJob).Replication.Serve.Ret.(*TCPServe).RPC.RxStructuredMaxLen)
	defConf := RPCConfig{}
	Default(&defConf)
	assert.Equal(t, defConf.Timeout, conf.Global.RPC.Timeout)
}

func TestGlobal_DefaultRPCConfig(t *testing.T) {
	assert.NotPanics(t, func() {
		var c RPCConfig
		Default(&c)
		assert.NotNil(t, c)
		assert.Equal(t, c.TxChunkSize, uint32(1)<<15)
	})
}
