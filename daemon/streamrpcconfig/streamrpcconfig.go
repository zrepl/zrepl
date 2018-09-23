package streamrpcconfig

import (
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/config"
)

func FromDaemonConfig(g *config.Global, in *config.RPCConfig) (*streamrpc.ConnConfig, error) {
	conf := in
	if conf == nil {
		conf = g.RPC
	}
	srpcConf := &streamrpc.ConnConfig{
		RxHeaderMaxLen:       conf.RxHeaderMaxLen,
		RxStructuredMaxLen:   conf.RxStructuredMaxLen,
		RxStreamMaxChunkSize: conf.RxStreamChunkMaxLen,
		TxChunkSize:          conf.TxChunkSize,
		Timeout: 			  conf.Timeout,
		SendHeartbeatInterval: conf.SendHeartbeatInterval,
	}
	if err := srpcConf.Validate(); err != nil {
		return nil, err
	}
	return srpcConf, nil
}
