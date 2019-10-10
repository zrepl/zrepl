package pruning

import "sort"

type KeepMostRecentCommonAncestor struct {
	_opaque struct{}
}

func NewKeepMostRecentCommonAncestor() *KeepMostRecentCommonAncestor {
	return &KeepMostRecentCommonAncestor{}
}

func (k *KeepMostRecentCommonAncestor) KeepRule(snaps []Snapshot) (destroyList []Snapshot) {
	var bothSides []Snapshot
	for _, s := range snaps {
		if s.PresentOnBothSides() {
			bothSides = append(bothSides, s)
		}
	}
	if len(bothSides) == 0 {
		return []Snapshot{}
	}

	sort.Slice(bothSides, func(i, j int) bool {
		return bothSides[i].Date().After(bothSides[j].Date())
	})

	return []Snapshot{bothSides[0]}
}
