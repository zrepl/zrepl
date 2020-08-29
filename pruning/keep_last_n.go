package pruning

import (
	"sort"
	"strings"

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
		// by date (youngest first)
		id, jd := res[i].Date(), res[j].Date()
		if !id.Equal(jd) {
			return id.After(jd)
		}
		// then lexicographically descending (e.g. b, a)
		return strings.Compare(res[i].Name(), res[j].Name()) == 1
	})

	return res[k.n:]
}
