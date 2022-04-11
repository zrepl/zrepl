package snapper

import (
	"context"
)

type manual struct{}

func (s *manual) Run(ctx context.Context, wakeUpCommon chan<- struct{}) {
	// nothing to do
}

func (s *manual) Report() Report {
	return Report{Type: TypeManual, Manual: &struct{}{}}
}
