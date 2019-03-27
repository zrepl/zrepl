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

func (k KeepLastN) KeepRule(snaps []Snapshot) (destroyList []Snapshot) {

	if k.n > len(snaps) {
		return []Snapshot{}
	}

	res := shallowCopySnapList(snaps)

	sort.Slice(res, func(i, j int) bool {
		return res[i].Date().After(res[j].Date())
	})

	return res[k.n:]
}
