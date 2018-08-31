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
	return &streamrpc.ConnConfig{
		RxHeaderMaxLen:       conf.RxHeaderMaxLen,
		RxStructuredMaxLen:   conf.RxStructuredMaxLen,
		RxStreamMaxChunkSize: conf.RxStreamChunkMaxLen,
		TxChunkSize:          conf.TxChunkSize,
		Timeout: streamrpc.Timeout{
			Progress: conf.Timeout,
		},
	}, nil
}
