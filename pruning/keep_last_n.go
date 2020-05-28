package pruning

import (
	"sort"

	"github.com/pkg/errors"
)

type KeepLastN struct {
	n int
}

func NewKeepLastN(n int) (*KeepLastN, error) {
	if n <= 0 {
		return nil, errors.Errorf("must specify positive number as 'keep last count', got %d", n)
	}
	return &KeepLastN{n}, nil
}

func (k KeepLastN) KeepRule(snaps []Snapshot) PruneSnapshotsResult {

	if k.n > len(snaps) {
		return PruneSnapshotsResult{Keep: snaps}
	}

	res := shallowCopySnapList(snaps)

	sort.Slice(res, func(i, j int) bool {
		return res[i].GetCreateTXG() > res[j].GetCreateTXG()
	})

	return PruneSnapshotsResult{Remove: res[k.n:], Keep: res[:k.n]}
}
