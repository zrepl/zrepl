package replication

import (
	"context"

	"github.com/zrepl/zrepl/cmd/replication/common"
	"github.com/zrepl/zrepl/cmd/replication/internal/mainfsm"
)

type Report = mainfsm.Report

type Replication interface {
	Drive(ctx context.Context, sender, receiver common.ReplicationEndpoint)
	Report() *Report
}

func NewReplication() Replication {
	return mainfsm.NewReplication()
}
