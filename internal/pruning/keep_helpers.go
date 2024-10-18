package pruning

func filterSnapList(snaps []Snapshot, predicate func(Snapshot) bool) []Snapshot {
	r := make([]Snapshot, 0, len(snaps))
	for i := range snaps {
		if predicate(snaps[i]) {
			r = append(r, snaps[i])
		}
	}
	return r
}

func partitionSnapList(snaps []Snapshot, predicate func(Snapshot) bool) (sTrue, sFalse []Snapshot) {
	for i := range snaps {
		if predicate(snaps[i]) {
			sTrue = append(sTrue, snaps[i])
		} else {
			sFalse = append(sFalse, snaps[i])
		}
	}
	return
}

func shallowCopySnapList(snaps []Snapshot) []Snapshot {
	c := make([]Snapshot, len(snaps))
	copy(c, snaps)
	return c
}
