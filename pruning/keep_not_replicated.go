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

func MustKeepNotReplicated(filesystems config.FilesystemsFilter) *KeepNotReplicated {
	k, err := NewKeepNotReplicated(&config.PruneKeepNotReplicated{
		PruneKeepCommon:      config.PruneKeepCommon{Filesystems: filesystems},
		KeepSnapshotAtCursor: false,
	})
	if err != nil {
		panic(err)
	}
	return k
}

func NewKeepNotReplicated(in *config.PruneKeepNotReplicated) (*KeepNotReplicated, error) {
	kc, err := newKeepCommon(&in.PruneKeepCommon)
	if err != nil {
		return nil, err
	}

	return &KeepNotReplicated{kc}, nil
}

func (k KeepNotReplicated) GetFSFilter() zfs.DatasetFilter {
	return k.fsf
}
