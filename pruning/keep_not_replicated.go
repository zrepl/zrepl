package pruning

import (
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/zfs"
)

type KeepNotReplicated struct {
	KeepCommon
}

func (*KeepNotReplicated) KeepRule(snaps []Snapshot) (destroyList []Snapshot) {
	return filterSnapList(snaps, func(snapshot Snapshot) bool {
		return snapshot.Replicated()
	})
}

func NewKeepNotReplicated(in *config.KeepNotReplicated) (*KeepNotReplicated, error) {
	kc, err := newKeepCommon(&in.KeepCommon)
	if err != nil {
		return nil, err
	}

	return &KeepNotReplicated{kc}, nil
}

func (k KeepNotReplicated) GetFSFilter() zfs.DatasetFilter {
	return k.fsf
}
