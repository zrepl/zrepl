package pruning

import (
	"github.com/zrepl/zrepl/config"
)

type KeepNotReplicated struct {
	KeepCommon
}

func (*KeepNotReplicated) KeepRule(snaps []Snapshot) (destroyList []Snapshot) {
	return filterSnapList(snaps, func(snapshot Snapshot) bool {
		return snapshot.Replicated()
	})
}

func NewKeepNotReplicated(in *config.PruneKeepNotReplicated) (*KeepNotReplicated, error) {
	kc, err := newKeepCommon(&in.PruneKeepCommon)
	if err != nil {
		return nil, err
	}

	return &KeepNotReplicated{kc}, nil
}
