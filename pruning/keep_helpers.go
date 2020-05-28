package pruning

func partitionSnapList(snaps []Snapshot, remove func(Snapshot) bool) (r PruneSnapshotsResult) {
	r.Keep = make([]Snapshot, 0, len(snaps))
	r.Remove = make([]Snapshot, 0, len(snaps))
	for i := range snaps {
		if remove(snaps[i]) {
			r.Remove = append(r.Remove, snaps[i])
		} else {
			r.Keep = append(r.Keep, snaps[i])
		}
	}
	return r
}

func shallowCopySnapList(snaps []Snapshot) []Snapshot {
	c := make([]Snapshot, len(snaps))
	copy(c, snaps)
	return c
}
