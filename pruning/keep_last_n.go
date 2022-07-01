package pruning

import (
	"sort"
	"strings"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/zfs"
)

type KeepLastN struct {
	KeepCommon
	n int
}

func NewKeepLastN(in *config.PruneKeepLastN) (*KeepLastN, error) {
	if in.Count <= 0 {
		return nil, errors.Errorf("must specify positive number as 'keep last count', got %d", in.Count)
	}

	kc, err := newKeepCommon(&in.PruneKeepCommon)
	if err != nil {
		return nil, err
	}

	return &KeepLastN{kc, in.Count}, nil
}

func (k KeepLastN) GetFSFilter() zfs.DatasetFilter {
	return k.fsf
}

func (k KeepLastN) KeepRule(snaps []Snapshot) (destroyList []Snapshot) {

	if k.n > len(snaps) {
		return []Snapshot{}
	}

	matching, notMatching := partitionSnapList(snaps, func(snapshot Snapshot) bool {
		return k.re.MatchString(snapshot.Name())
	})
	// snaps that don't match the regex are not kept by this rule
	destroyList = append(destroyList, notMatching...)

	if len(matching) == 0 {
		return destroyList
	}

	sort.Slice(matching, func(i, j int) bool {
		// by date (youngest first)
		id, jd := matching[i].Date(), matching[j].Date()
		if !id.Equal(jd) {
			return id.After(jd)
		}
		// then lexicographically descending (e.g. b, a)
		return strings.Compare(matching[i].Name(), matching[j].Name()) == 1
	})

	n := k.n
	if n > len(matching) {
		n = len(matching)
	}
	destroyList = append(destroyList, matching[n:]...)
	return destroyList
}
