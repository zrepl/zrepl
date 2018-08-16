package replication

import (
	"context"

	"github.com/zrepl/zrepl/cmd/replication/common"
	"github.com/zrepl/zrepl/cmd/replication/internal/mainfsm"
)

type Report = mainfsm.Report

type Replication interface {
	Drive(ctx context.Context, ep common.EndpointPair)
	Report() *Report
}

func NewReplication() Replication {
	return mainfsm.NewReplication()
}
