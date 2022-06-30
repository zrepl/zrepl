package pruning

import (
	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
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
	k, err := NewKeepNotReplicated(filesystems)
	if err != nil {
		panic(err)
	}
	return k
}

func NewKeepNotReplicated(filesystems config.FilesystemsFilter) (*KeepNotReplicated, error) {
	fsf, err := filters.DatasetMapFilterFromConfig(filesystems)
	if err != nil {
		return nil, errors.Errorf("invalid filesystems: %s", err)
	}
	return &KeepNotReplicated{KeepCommon{nil, fsf}}, nil
}

func (k KeepNotReplicated) GetFSFilter() zfs.DatasetFilter {
	return k.fsf
}
